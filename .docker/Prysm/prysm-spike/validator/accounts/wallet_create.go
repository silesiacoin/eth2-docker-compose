package accounts

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/manifoldco/promptui"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/shared/promptutil"
	"github.com/prysmaticlabs/prysm/validator/accounts/prompt"
	"github.com/prysmaticlabs/prysm/validator/accounts/wallet"
	"github.com/prysmaticlabs/prysm/validator/flags"
	"github.com/prysmaticlabs/prysm/validator/keymanager"
	"github.com/prysmaticlabs/prysm/validator/keymanager/derived"
	"github.com/prysmaticlabs/prysm/validator/keymanager/remote"
	"github.com/urfave/cli/v2"
)

// CreateWalletConfig defines the parameters needed to call the create wallet functions.
type CreateWalletConfig struct {
	WalletCfg            *wallet.Config
	RemoteKeymanagerOpts *remote.KeymanagerOpts
	SkipMnemonicConfirm  bool
	Mnemonic25thWord     string
	NumAccounts          int
}

// CreateAndSaveWalletCli from user input with a desired keymanager. If a
// wallet already exists in the path, it suggests the user alternatives
// such as how to edit their existing wallet configuration.
func CreateAndSaveWalletCli(cliCtx *cli.Context) (*wallet.Wallet, error) {
	keymanagerKind, err := extractKeymanagerKindFromCli(cliCtx)
	if err != nil {
		return nil, err
	}
	createWalletConfig, err := extractWalletCreationConfigFromCli(cliCtx, keymanagerKind)
	if err != nil {
		return nil, err
	}

	dir := createWalletConfig.WalletCfg.WalletDir
	dirExists, err := wallet.Exists(dir)
	if err != nil {
		return nil, err
	}
	if dirExists {
		return nil, errors.New("a wallet already exists at this location. Please input an" +
			" alternative location for the new wallet or remove the current wallet")
	}

	w, err := CreateWalletWithKeymanager(cliCtx.Context, createWalletConfig)
	if err != nil {
		return nil, errors.Wrap(err, "could not create wallet")
	}
	return w, nil
}

// CreateWalletWithKeymanager specified by configuration options.
func CreateWalletWithKeymanager(ctx context.Context, cfg *CreateWalletConfig) (*wallet.Wallet, error) {
	w := wallet.New(&wallet.Config{
		WalletDir:      cfg.WalletCfg.WalletDir,
		KeymanagerKind: cfg.WalletCfg.KeymanagerKind,
		WalletPassword: cfg.WalletCfg.WalletPassword,
	})
	var err error
	switch w.KeymanagerKind() {
	case keymanager.Imported:
		if err = createImportedKeymanagerWallet(ctx, w); err != nil {
			return nil, errors.Wrap(err, "could not initialize wallet")
		}
		log.WithField("--wallet-dir", cfg.WalletCfg.WalletDir).Info(
			"Successfully created wallet with ability to import keystores",
		)
	case keymanager.Derived:
		if err = createDerivedKeymanagerWallet(
			ctx,
			w,
			cfg.Mnemonic25thWord,
			cfg.SkipMnemonicConfirm,
			cfg.NumAccounts,
		); err != nil {
			return nil, errors.Wrap(err, "could not initialize wallet")
		}
		log.WithField("--wallet-dir", cfg.WalletCfg.WalletDir).Info(
			"Successfully created HD wallet from mnemonic and regenerated accounts",
		)
	case keymanager.Remote:
		if err = createRemoteKeymanagerWallet(ctx, w, cfg.RemoteKeymanagerOpts); err != nil {
			return nil, errors.Wrap(err, "could not initialize wallet")
		}
		log.WithField("--wallet-dir", cfg.WalletCfg.WalletDir).Info(
			"Successfully created wallet with remote keymanager configuration",
		)
	default:
		return nil, errors.Wrapf(err, "keymanager type %s is not supported", w.KeymanagerKind())
	}
	return w, nil
}

func extractKeymanagerKindFromCli(cliCtx *cli.Context) (keymanager.Kind, error) {
	return inputKeymanagerKind(cliCtx)
}

func extractWalletCreationConfigFromCli(cliCtx *cli.Context, keymanagerKind keymanager.Kind) (*CreateWalletConfig, error) {
	walletDir, err := prompt.InputDirectory(cliCtx, prompt.WalletDirPromptText, flags.WalletDirFlag)
	if err != nil {
		return nil, err
	}
	walletPassword, err := promptutil.InputPassword(
		cliCtx,
		flags.WalletPasswordFileFlag,
		wallet.NewWalletPasswordPromptText,
		wallet.ConfirmPasswordPromptText,
		true, /* Should confirm password */
		promptutil.ValidatePasswordInput,
	)
	if err != nil {
		return nil, err
	}
	createWalletConfig := &CreateWalletConfig{
		WalletCfg: &wallet.Config{
			WalletDir:      walletDir,
			KeymanagerKind: keymanagerKind,
			WalletPassword: walletPassword,
		},
		SkipMnemonicConfirm: cliCtx.Bool(flags.SkipDepositConfirmationFlag.Name),
	}
	skipMnemonic25thWord := cliCtx.IsSet(flags.SkipMnemonic25thWordCheckFlag.Name)
	has25thWordFile := cliCtx.IsSet(flags.Mnemonic25thWordFileFlag.Name)
	if keymanagerKind == keymanager.Derived {
		numAccounts, err := inputNumAccounts(cliCtx)
		if err != nil {
			return nil, errors.Wrap(err, "could not get number of accounts to generate")
		}
		createWalletConfig.NumAccounts = int(numAccounts)
	}
	if keymanagerKind == keymanager.Derived && !skipMnemonic25thWord && !has25thWordFile {
		resp, err := promptutil.ValidatePrompt(
			os.Stdin, newMnemonicPassphraseYesNoText, promptutil.ValidateYesOrNo,
		)
		if err != nil {
			return nil, errors.Wrap(err, "could not validate choice")
		}
		if strings.ToLower(resp) == "y" {
			mnemonicPassphrase, err := promptutil.InputPassword(
				cliCtx,
				flags.Mnemonic25thWordFileFlag,
				newMnemonicPassphrasePromptText,
				"Confirm mnemonic passphrase",
				true, /* Should confirm password */
				func(input string) error {
					if strings.TrimSpace(input) == "" {
						return errors.New("input cannot be empty")
					}
					return nil
				},
			)
			if err != nil {
				return nil, err
			}
			createWalletConfig.Mnemonic25thWord = mnemonicPassphrase
		}
	}
	if keymanagerKind == keymanager.Remote {
		opts, err := prompt.InputRemoteKeymanagerConfig(cliCtx)
		if err != nil {
			return nil, errors.Wrap(err, "could not input remote keymanager config")
		}
		createWalletConfig.RemoteKeymanagerOpts = opts
	}
	return createWalletConfig, nil
}

func createImportedKeymanagerWallet(ctx context.Context, wallet *wallet.Wallet) error {
	if wallet == nil {
		return errors.New("nil wallet")
	}
	if err := wallet.SaveWallet(); err != nil {
		return errors.Wrap(err, "could not save wallet to disk")
	}
	return nil
}

func createDerivedKeymanagerWallet(
	ctx context.Context,
	wallet *wallet.Wallet,
	mnemonicPassphrase string,
	skipMnemonicConfirm bool,
	numAccounts int,
) error {
	if wallet == nil {
		return errors.New("nil wallet")
	}
	if err := wallet.SaveWallet(); err != nil {
		return errors.Wrap(err, "could not save wallet to disk")
	}
	km, err := derived.NewKeymanager(ctx, &derived.SetupConfig{
		Wallet: wallet,
	})
	if err != nil {
		return errors.Wrap(err, "could not initialize HD keymanager")
	}
	mnemonic, err := derived.GenerateAndConfirmMnemonic(skipMnemonicConfirm)
	if err != nil {
		return errors.Wrap(err, "could not confirm mnemonic")
	}
	if err := km.RecoverAccountsFromMnemonic(ctx, mnemonic, mnemonicPassphrase, numAccounts); err != nil {
		return errors.Wrap(err, "could not recover accounts from mnemonic")
	}
	return nil
}

func createRemoteKeymanagerWallet(ctx context.Context, wallet *wallet.Wallet, opts *remote.KeymanagerOpts) error {
	keymanagerConfig, err := remote.MarshalOptionsFile(ctx, opts)
	if err != nil {
		return errors.Wrap(err, "could not marshal config file")
	}
	if err := wallet.SaveWallet(); err != nil {
		return errors.Wrap(err, "could not save wallet to disk")
	}
	if err := wallet.WriteKeymanagerConfigToDisk(ctx, keymanagerConfig); err != nil {
		return errors.Wrap(err, "could not write keymanager config to disk")
	}
	return nil
}

func inputKeymanagerKind(cliCtx *cli.Context) (keymanager.Kind, error) {
	if cliCtx.IsSet(flags.KeymanagerKindFlag.Name) {
		return keymanager.ParseKind(cliCtx.String(flags.KeymanagerKindFlag.Name))
	}
	promptSelect := promptui.Select{
		Label: "Select a type of wallet",
		Items: []string{
			wallet.KeymanagerKindSelections[keymanager.Imported],
			wallet.KeymanagerKindSelections[keymanager.Derived],
			wallet.KeymanagerKindSelections[keymanager.Remote],
		},
	}
	selection, _, err := promptSelect.Run()
	if err != nil {
		return keymanager.Imported, fmt.Errorf("could not select wallet type: %v", prompt.FormatPromptError(err))
	}
	return keymanager.Kind(selection), nil
}
