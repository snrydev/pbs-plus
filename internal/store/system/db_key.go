package system

import (
	"os"

	"github.com/pbs-plus/pbs-plus/internal/store/constants"
)

func GetDBKey() ([]byte, error) {
	fileContent, err := os.ReadFile(constants.DatabaseSecretsFile)
	if err != nil {
		return nil, err
	}

	return fileContent, nil
}
