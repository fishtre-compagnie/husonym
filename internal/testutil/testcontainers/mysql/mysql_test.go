package testcontainers_mysql

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_knowsRestrictFkOnNonStandardKey(t *testing.T) {
	for image, expected := range map[string]bool{
		defaultMysqlImage:  true,
		"mysql:8.4":        true,
		"mysql:8":          true,
		"mysql:9.1.0":      true,
		"mysql:latest":     true,
		"mysql:lts":        true,
		"mysql:8.0":        false,
		"mysql:8.0.46":     false,
		"mysql:8.0-debian": false,
		Mysql57Image:       false,
		"mysql:5.7":        false,
		"mariadb:11.4":     false,
	} {
		require.Equal(t, expected, knowsRestrictFkOnNonStandardKey(image), image)
	}
}

func Test_IsMysql57(t *testing.T) {
	for image, expected := range map[string]bool{
		Mysql57Image:       true,
		"mysql:5.7":        true,
		"mysql:5.7-debian": true,
		"mysql:5":          false,
		"mysql:8.0":        false,
		defaultMysqlImage:  false,
		"mariadb:11.4":     false,
	} {
		require.Equal(t, expected, IsMysql57(image), image)
	}
}

func Test_WithMysql57(t *testing.T) {
	cfg := &mysqlTestContainerConfig{image: defaultMysqlImage}
	WithMysql57()(cfg)
	WithServerArgs("--max-connections=500")(cfg)
	require.Equal(t, Mysql57Image, cfg.image)
	require.Equal(t, append(append([]string{}, mysql57ServerArgs...), "--max-connections=500"), cfg.serverArgs)
}

func Test_tlsVersions(t *testing.T) {
	require.Equal(t, "TLSv1.2", tlsVersions(Mysql57Image))
	require.Equal(t, "TLSv1.2,TLSv1.3", tlsVersions(defaultMysqlImage))
	require.Equal(t, "TLSv1.2,TLSv1.3", tlsVersions("mariadb:11.4"))
}
