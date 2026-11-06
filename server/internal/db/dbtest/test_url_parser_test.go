package dbtest

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// This regression only parses anonymous URLs. It never opens a database connection or invokes the server tests.
func TestServerTestDatabaseURLParsing(t *testing.T) {
	if os.Getenv("SERVER_TEST_DB_PARSER_CHILD") == "1" {
		for _, entry := range os.Environ() {
			if strings.HasPrefix(entry, "PG") {
				t.Fatal("PostgreSQL runtime environment reached the isolated driver parser")
			}
		}
		current, legacy := os.Getenv("PERSONA_TEST_DATABASE_URL"), os.Getenv(Env)
		if current == "" || current != legacy {
			t.Fatal("dedicated test database aliases differ")
		}
		cfg, err := pgconn.ParseConfig(current)
		if err != nil {
			t.Fatalf("anonymous isolated driver parse failed: %v", err)
		}
		if cfg.Database != "persona_test" || cfg.User != "fixture" || cfg.Password != "synthetic" || cfg.Host != "127.0.0.1" {
			t.Fatal("isolated driver did not retain the explicit anonymous test target")
		}
		return
	}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "PG") {
			t.Setenv(key, "")
		}
	}
	const noProfile = "&passfile=&sslcert=&sslkey=&sslrootcert="
	for _, option := range []string{"dbname=persona", "database=persona", "db%6Eame=persona", "dbname=persona&dbname=persona_test"} {
		t.Run("driver-overrides-path/"+option, func(t *testing.T) {
			cfg, err := pgconn.ParseConfig("postgres://fixture:synthetic@127.0.0.1/persona_test?" + option + noProfile)
			if err != nil {
				t.Fatalf("anonymous driver parse failed: %v", err)
			}
			if cfg.Database != "persona" {
				t.Fatalf("driver database = %q; update the isolation regression if precedence changes", cfg.Database)
			}
		})
	}

	launcher, err := filepath.Abs("../../../../scripts/test/server_test_env.py")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(launcher); os.IsNotExist(err) {
		// The remote server-test checkout receives the launcher alongside server/, not the complete scripts tree.
		launcher, err = filepath.Abs("../../../../server_test_env.py")
		if err != nil {
			t.Fatal(err)
		}
	}
	const python = `import importlib.util,sys
spec=importlib.util.spec_from_file_location("server_test_env",sys.argv[1])
module=importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
try:
 print(module.test_database_url(sys.argv[2]))
except ValueError:
 sys.exit(2)
`
	// Generate an anonymous certificate locally; no service, account key, or network listener is involved.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "fixture.invalid"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true,
		KeyUsage:    x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	certificate, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certDir := t.TempDir()
	certFile, keyFile := filepath.Join(certDir, "fixture.crt"), filepath.Join(certDir, "fixture.key")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate}), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0600); err != nil {
		t.Fatal(err)
	}
	explicitRoot := url.QueryEscape(certFile)
	explicitKey := url.QueryEscape(keyFile)
	cases := []struct {
		name, source string
		accept       bool
	}{
		{"no-query", "postgres://fixture:synthetic@127.0.0.1/persona", true},
		{"postgresql", "postgresql://fixture:synthetic@[::1]:5432/persona?sslmode=disable", true},
		{"tls-require", "postgres://fixture:synthetic@127.0.0.1/persona?sslmode=require", true},
		{"tls-allow", "postgres://fixture:synthetic@127.0.0.1/persona?sslmode=allow", true},
		{"tls-prefer", "postgres://fixture:synthetic@127.0.0.1/persona?sslmode=prefer", true},
		{"tls-verify-ca", "postgres://fixture:synthetic@127.0.0.1/persona?sslmode=verify-ca", true},
		{"tls-verify-full", "postgres://fixture:synthetic@127.0.0.1/persona?sslmode=verify-full", true},
		{"tls-explicit-root", "postgres://fixture:synthetic@127.0.0.1/persona?sslmode=require&sslrootcert=" + explicitRoot, true},
		{"tls-explicit-client", "postgres://fixture:synthetic@127.0.0.1/persona?sslmode=verify-full&sslrootcert=" + explicitRoot + "&sslcert=" + explicitRoot + "&sslkey=" + explicitKey, true},

		{"encoded-password", "postgres://fixture:p%40ss@127.0.0.1/persona?sslmode=disable", true},
		{"encoded-safe-key", "postgres://fixture:synthetic@127.0.0.1/persona?ssl%6dode=disable", true},
		{"timeout", "postgres://fixture:synthetic@127.0.0.1/persona?connect_timeout=3&application_name=fixture", true},
		{"target-attrs", "postgres://fixture:synthetic@127.0.0.1/persona?target_session_attrs=read-write", true},
		{"empty-certificates", "postgres://fixture:synthetic@127.0.0.1/persona?sslcert=&sslkey=&sslrootcert=", true},
		{"dbname", "postgres://fixture:synthetic@127.0.0.1/persona?dbname=persona", false},
		{"database", "postgres://fixture:synthetic@127.0.0.1/persona?database=persona", false},
		{"encoded-dbname", "postgres://fixture:synthetic@127.0.0.1/persona?db%6Eame=persona", false},
		{"encoded-database", "postgres://fixture:synthetic@127.0.0.1/persona?%64atabase=persona", false},
		{"mixed-case-dbname", "postgres://fixture:synthetic@127.0.0.1/persona?DbName=persona", false},
		{"duplicate-dbname", "postgres://fixture:synthetic@127.0.0.1/persona?dbname=persona_test&dbname=persona", false},
		{"alias-overrides", "postgres://fixture:synthetic@127.0.0.1/persona?database=persona&dbname=persona_test", false},
		{"service", "postgres://fixture:synthetic@127.0.0.1/persona?service=fixture", false},
		{"encoded-service", "postgres://fixture:synthetic@127.0.0.1/persona?serv%69ce=fixture", false},
		{"servicefile", "postgres://fixture:synthetic@127.0.0.1/persona?servicefile=/synthetic/service", false},
		{"passfile", "postgres://fixture:synthetic@127.0.0.1/persona?passfile=/synthetic/passfile", false},
		{"host", "postgres://fixture:synthetic@127.0.0.1/persona?host=example.invalid", false},
		{"unknown-options", "postgres://fixture:synthetic@127.0.0.1/persona?options=fixture", false},
		{"duplicate-safe-key", "postgres://fixture:synthetic@127.0.0.1/persona?sslmode=disable&ssl%6dode=require", false},
		{"bad-escape", "postgres://fixture:synthetic@127.0.0.1/persona?sslmode=%xx", false},
		{"bad-utf8", "postgres://fixture:synthetic@127.0.0.1/persona?sslmode=%ff", false},
		{"semicolon", "postgres://fixture:synthetic@127.0.0.1/persona?sslmode=disable;dbname=persona", false},
		{"missing-equals", "postgres://fixture:synthetic@127.0.0.1/persona?sslmode", false},
		{"fragment", "postgres://fixture:synthetic@127.0.0.1/persona#dbname=persona", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			config := filepath.Join(t.TempDir(), "hub.json")
			raw, err := json.Marshal(map[string]string{"database_url": tc.source})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(config, raw, 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "python3", "-I", "-c", python, launcher, config)
			output, err := cmd.Output()
			if !tc.accept {
				if failure, ok := err.(*exec.ExitError); !ok || failure.ExitCode() != 2 || len(output) != 0 {
					t.Fatalf("unsafe anonymous input not rejected: output %q, err %v", output, err)
				}
				return
			}
			if err != nil {
				if failure, ok := err.(*exec.ExitError); ok {
					t.Fatalf("anonymous launcher input failed: %v: %s", err, failure.Stderr)
				}
				t.Fatalf("anonymous launcher input failed: %v", err)
			}
			cfg, err := pgconn.ParseConfig(strings.TrimSpace(string(output)))
			if err != nil {
				t.Fatalf("anonymous sanitized URL failed driver parse: %v", err)
			}
			if cfg.Database != "persona_test" {
				t.Fatalf("actual driver database = %q, not the dedicated test database", cfg.Database)
			}
			if cfg.User != "fixture" || cfg.Password == "" {
				t.Fatal("explicit anonymous authentication was not retained")
			}
			switch tc.name {
			case "postgresql", "encoded-password", "encoded-safe-key":
				if cfg.TLSConfig != nil {
					t.Fatal("sslmode=disable unexpectedly enabled TLS")
				}
			case "tls-allow":
				if cfg.TLSConfig != nil || len(cfg.Fallbacks) != 1 || cfg.Fallbacks[0].TLSConfig == nil {
					t.Fatal("sslmode=allow ordering changed")
				}
			case "no-query", "tls-prefer":
				if cfg.TLSConfig == nil || len(cfg.Fallbacks) != 1 || cfg.Fallbacks[0].TLSConfig != nil {
					t.Fatal("sslmode=prefer ordering changed")
				}
			case "tls-require":
				if cfg.TLSConfig == nil || len(cfg.Fallbacks) != 0 {
					t.Fatal("sslmode=require no longer requires TLS")
				}
			case "tls-verify-ca", "tls-explicit-root":
				if cfg.TLSConfig == nil || cfg.TLSConfig.VerifyPeerCertificate == nil || len(cfg.Fallbacks) != 0 {
					t.Fatal("certificate chain validation was removed")
				}
				if err := cfg.TLSConfig.VerifyPeerCertificate([][]byte{[]byte("not a certificate")}, nil); err == nil {
					t.Fatal("certificate chain validation accepted an invalid certificate")
				}
			case "tls-verify-full", "tls-explicit-client":
				if cfg.TLSConfig == nil || cfg.TLSConfig.InsecureSkipVerify || cfg.TLSConfig.ServerName != "127.0.0.1" || len(cfg.Fallbacks) != 0 {
					t.Fatal("full certificate/hostname verification was weakened")
				}
			}
			if tc.name == "tls-explicit-client" && (len(cfg.TLSConfig.Certificates) != 1 || cfg.TLSConfig.RootCAs == nil) {
				t.Fatal("explicit anonymous TLS certificate/key/root parameters were not retained")
			}
		})
	}
	t.Run("isolated-environment-with-real-driver", func(t *testing.T) {
		directory := t.TempDir()
		service := filepath.Join(directory, "service.conf")
		if err := os.WriteFile(service, []byte("[live]\ndbname=persona\nhost=example.invalid\nuser=live-user\n"), 0600); err != nil {
			t.Fatal(err)
		}
		for key, value := range map[string]string{
			"PGDATABASE": "persona", "PGUSER": "live-user", "PGPASSWORD": "synthetic-live-password",
			"PGHOST": "example.invalid", "PGSERVICE": "live", "PGSERVICEFILE": service,
			"PGPASSFILE": filepath.Join(directory, "passwords"), "PGOPTIONS": "synthetic",
			"SERVER_TEST_DB_PARSER_CHILD": "1",
		} {
			t.Setenv(key, value)
		}
		config := filepath.Join(directory, "hub.json")
		if err := os.WriteFile(config, []byte(`{"database_url":"postgres://fixture:synthetic@127.0.0.1/persona?sslmode=disable"}`), 0600); err != nil {
			t.Fatal(err)
		}
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		const runPython = `import importlib.util,sys
spec=importlib.util.spec_from_file_location("server_test_env",sys.argv[1])
module=importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
sys.exit(module.run_tests(sys.argv[2],[sys.argv[3],"-test.run=^TestServerTestDatabaseURLParsing$","-test.v"]))
`
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		output, err := exec.CommandContext(ctx, "python3", "-I", "-c", runPython, launcher, config, executable).CombinedOutput()
		if err != nil {
			t.Fatalf("anonymous isolated parser subprocess failed: %v\n%s", err, output)
		}
		if !strings.Contains(string(output), "--- PASS: TestServerTestDatabaseURLParsing") {
			t.Fatal("isolated driver parser did not execute")
		}
	})
}
