package main

import (
	"context"
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	vsphere "github.com/embano1/vsphere/client"
	"github.com/vmware/govmomi/simulator"
	_ "github.com/vmware/govmomi/vapi/simulator"
	"github.com/vmware/govmomi/vim25"
	"go.uber.org/zap/zaptest"
	"gotest.tools/assert"
	"knative.dev/pkg/logging"
)

func Test_run(t *testing.T) {
	const (
		username = "administrator@vsphere.local"
		password = "passw0rd"
	)

	secretsDir := createSecret(t, username, password)
	defaultEnv := envConfig{
		Config: vsphere.Config{
			SecretPath: secretsDir,
			Insecure:   true, // vcsim
		},
		Port:        50001,
		EventSuffix: "AlarmInfo",
		InjectKey:   "AlarmInfo",
	}

	t.Run("fail with authentication error", func(t *testing.T) {
		model := simulator.VPX()

		defer model.Remove()
		err := model.Create()
		assert.NilError(t, err)

		model.Service.Listen = &url.URL{
			User: url.UserPassword("not-my-username", password),
		}

		simulator.Run(func(ctx context.Context, client *vim25.Client) error {
			defaultEnv.Config.Address = client.URL().String()
			err := setEnv(defaultEnv)
			assert.NilError(t, err)

			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			ctx = logging.WithLogger(ctx, zaptest.NewLogger(t).Sugar())

			go func() {
				<-time.After(time.Second)
				logging.FromContext(ctx).Debug("stopping test server")
				cancel()
			}()

			err = run(ctx, nil)
			assert.ErrorContains(t, err, "ServerFaultCode: Login failure")
			return nil
		}, model)
	})

	t.Run("successfully connect to vcenter", func(t *testing.T) {
		model := simulator.VPX()

		defer model.Remove()
		err := model.Create()
		assert.NilError(t, err)

		model.Service.Listen = &url.URL{
			User: url.UserPassword(username, password),
		}

		simulator.Run(func(ctx context.Context, client *vim25.Client) error {
			defaultEnv.Config.Address = client.URL().String()
			err := setEnv(defaultEnv)
			assert.NilError(t, err)

			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			ctx = logging.WithLogger(ctx, zaptest.NewLogger(t).Sugar())

			go func() {
				<-time.After(time.Second)
				logging.FromContext(ctx).Debug("stopping test server")
				cancel()
			}()

			err = run(ctx, nil)
			assert.Error(t, err, "context canceled")
			return nil
		}, model)
	})

}

// createSecret returns a directory with username and password files holding
// user/pass credentials
func createSecret(t *testing.T, username, password string) string {
	t.Helper()
	dir, err := ioutil.TempDir("", "k8s-secret")
	if err != nil {
		t.Fatalf("create secrets directory: %v", err)
	}

	t.Cleanup(func() {
		if err = os.RemoveAll(dir); err != nil {
			t.Errorf("cleanup temp directory: %v", err)
		}
	})

	userFile := filepath.Join(dir, "username")
	err = ioutil.WriteFile(userFile, []byte(username), 0444)
	if err != nil {
		t.Fatalf("create username secret: %v", err)
	}

	passFile := filepath.Join(dir, "password")
	err = ioutil.WriteFile(passFile, []byte(password), 0444)
	if err != nil {
		t.Fatalf("create password secret: %v", err)
	}

	return dir
}

// setEnv sets all environment variables defined in env
func setEnv(env envConfig) error {
	if err := os.Setenv("PORT", strconv.Itoa(env.Port)); err != nil {
		return err
	}

	if err := os.Setenv("VCENTER_URL", env.Config.Address); err != nil {
		return err
	}

	if err := os.Setenv("VCENTER_INSECURE", strconv.FormatBool(env.Insecure)); err != nil {
		return err
	}

	if err := os.Setenv("VCENTER_SECRET_PATH", env.SecretPath); err != nil {
		return err
	}

	if err := os.Setenv("EVENT_SUFFIX", env.EventSuffix); err != nil {
		return err
	}

	if err := os.Setenv("ALARM_KEY", env.InjectKey); err != nil {
		return err
	}

	if err := os.Setenv("CACHE_TTL", strconv.Itoa(int(env.TTL))); err != nil {
		return err
	}

	if err := os.Setenv("DEBUG", strconv.FormatBool(env.Debug)); err != nil {
		return err
	}

	return nil
}
