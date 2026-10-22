package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"

	"github.com/LucasLuchetta/Damocline/internal/capacityplan"
)

const maxAuditorExecutableBytes = 1 << 30

func currentAuditorIdentity() (capacityplan.AuditorIdentity, error) {
	executable, err := os.Executable()
	if err != nil {
		return capacityplan.AuditorIdentity{}, fmt.Errorf("locate auditor executable: %w", err)
	}
	file, err := os.Open(executable)
	if err != nil {
		return capacityplan.AuditorIdentity{}, fmt.Errorf("open auditor executable: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return capacityplan.AuditorIdentity{}, fmt.Errorf("inspect auditor executable: %w", err)
	}
	if info.Size() < 1 || info.Size() > maxAuditorExecutableBytes {
		return capacityplan.AuditorIdentity{}, fmt.Errorf("auditor executable size %d is outside the 1-byte to %d-byte limit", info.Size(), maxAuditorExecutableBytes)
	}
	hash := sha256.New()
	read, err := io.Copy(hash, io.LimitReader(file, maxAuditorExecutableBytes+1))
	if err != nil {
		return capacityplan.AuditorIdentity{}, fmt.Errorf("hash auditor executable: %w", err)
	}
	if read != info.Size() {
		return capacityplan.AuditorIdentity{}, fmt.Errorf("auditor executable changed while it was being hashed")
	}
	return capacityplan.AuditorIdentity{Version: Version, ExecutableSHA256: "sha256:" + hex.EncodeToString(hash.Sum(nil))}, nil
}

func validAuditorSHA256(value string) bool {
	if len(value) != len("sha256:")+64 || value[:len("sha256:")] != "sha256:" {
		return false
	}
	for _, char := range value[len("sha256:"):] {
		if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f') {
			return false
		}
	}
	return true
}
