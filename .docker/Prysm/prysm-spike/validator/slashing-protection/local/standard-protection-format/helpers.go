package interchangeformat

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/k0kubun/go-ansi"
	"github.com/schollz/progressbar/v3"
)

func initializeProgressBar(numItems int, msg string) *progressbar.ProgressBar {
	return progressbar.NewOptions(
		numItems,
		progressbar.OptionFullWidth(),
		progressbar.OptionSetWriter(ansi.NewAnsiStdout()),
		progressbar.OptionEnableColorCodes(true),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "[green]=[reset]",
			SaucerHead:    "[green]>[reset]",
			SaucerPadding: " ",
			BarStart:      "[",
			BarEnd:        "]",
		}),
		progressbar.OptionOnCompletion(func() { fmt.Println() }),
		progressbar.OptionSetDescription(msg),
	)
}

func uint64FromString(str string) (uint64, error) {
	return strconv.ParseUint(str, 10, 64)
}

func pubKeyFromHex(str string) ([48]byte, error) {
	pubKeyBytes, err := hex.DecodeString(strings.TrimPrefix(str, "0x"))
	if err != nil {
		return [48]byte{}, err
	}
	if len(pubKeyBytes) != 48 {
		return [48]byte{}, fmt.Errorf("public key is not correct, 48-byte length: %s", str)
	}
	var pk [48]byte
	copy(pk[:], pubKeyBytes[:48])
	return pk, nil
}

func rootFromHex(str string) ([32]byte, error) {
	rootHexBytes, err := hex.DecodeString(strings.TrimPrefix(str, "0x"))
	if err != nil {
		return [32]byte{}, err
	}
	if len(rootHexBytes) != 32 {
		return [32]byte{}, fmt.Errorf("wrong root length, 32-byte length: %s", str)
	}
	var root [32]byte
	copy(root[:], rootHexBytes[:32])
	return root, nil
}

func rootToHexString(root []byte) (string, error) {
	// Nil signing roots are allowed in EIP-3076.
	if len(root) == 0 {
		return "", nil
	}
	if len(root) != 32 {
		return "", fmt.Errorf("wanted length 32, received %d", len(root))
	}
	return fmt.Sprintf("%#x", root), nil
}

func pubKeyToHexString(pubKey []byte) (string, error) {
	if len(pubKey) != 48 {
		return "", fmt.Errorf("wanted length 48, received %d", len(pubKey))
	}
	return fmt.Sprintf("%#x", pubKey), nil
}
