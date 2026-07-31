// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

//go:build libsodium
// +build libsodium

package internal

import (
	"github.com/pkg/errors"
	"github.com/spf13/viper"

	conf "github.com/lateos-ai/wal-g/internal/config"
	"github.com/lateos-ai/wal-g/internal/crypto"
	"github.com/lateos-ai/wal-g/internal/crypto/libsodium"
)

func configureLibsodiumCrypter(config *viper.Viper) (crypto.Crypter, error) {
	if viper.IsSet(conf.LibsodiumKeySetting) {
		return libsodium.CrypterFromKey(viper.GetString(conf.LibsodiumKeySetting), viper.GetString(conf.LibsodiumKeyTransform)), nil
	}

	if viper.IsSet(conf.LibsodiumKeyPathSetting) {
		return libsodium.CrypterFromKeyPath(viper.GetString(conf.LibsodiumKeyPathSetting), viper.GetString(conf.LibsodiumKeyTransform)), nil
	}

	return nil, errors.New("there is no any supported libsodium crypter configuration")
}
