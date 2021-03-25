package main

import (
	"context"
	"io/ioutil"
	"net/url"
	"path/filepath"
	"time"

	"github.com/kelseyhightower/envconfig"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/session"
	"github.com/vmware/govmomi/session/keepalive"
	"github.com/vmware/govmomi/vim25"
	"github.com/vmware/govmomi/vim25/methods"
	"github.com/vmware/govmomi/vim25/soap"
	"github.com/vmware/govmomi/vim25/types"
	v1 "k8s.io/api/core/v1"
	"knative.dev/pkg/logging"
)

const (
	keepaliveInterval = 5 * time.Minute
)

// newSOAPClient returns a vCenter SOAP API client with active keep-alive. Use
// Logout() to release resources and perform a clean logout from vCenter.
func newSOAPClient(ctx context.Context) (*govmomi.Client, error) {
	var env envConfig
	if err := envconfig.Process(envPrefix, &env); err != nil {
		return nil, err
	}

	parsedURL, err := soap.ParseURL(env.VCenter)
	if err != nil {
		return nil, err
	}

	// Read the username and password from the filesystem.
	username, err := readKey(v1.BasicAuthUsernameKey)
	if err != nil {
		return nil, err
	}
	password, err := readKey(v1.BasicAuthPasswordKey)
	if err != nil {
		return nil, err
	}
	parsedURL.User = url.UserPassword(username, password)

	return soapWithKeepalive(ctx, parsedURL, env.Insecure)
}

// ReadKey reads the key from the secret.
func readKey(key string) (string, error) {
	var env envConfig
	if err := envconfig.Process(envPrefix, &env); err != nil {
		return "", err
	}

	mountPath := defaultMountPath
	if env.SecretPath != "" {
		mountPath = env.SecretPath
	}

	data, err := ioutil.ReadFile(filepath.Join(mountPath, key))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func soapWithKeepalive(ctx context.Context, url *url.URL, insecure bool) (*govmomi.Client, error) {
	soapClient := soap.NewClient(url, insecure)
	vimClient, err := vim25.NewClient(ctx, soapClient)
	if err != nil {
		return nil, err
	}
	vimClient.RoundTripper = keepalive.NewHandlerSOAP(vimClient.RoundTripper, keepaliveInterval, soapKeepAliveHandler(ctx, vimClient))

	// explicitly create session to activate keep-alive handler via Login
	m := session.NewManager(vimClient)
	err = m.Login(ctx, url.User)
	if err != nil {
		return nil, err
	}

	c := govmomi.Client{
		Client:         vimClient,
		SessionManager: m,
	}

	return &c, nil
}

func soapKeepAliveHandler(ctx context.Context, c *vim25.Client) func() error {
	logger := logging.FromContext(ctx).With("rpc", "keepalive")

	return func() error {
		logger.Info("Executing SOAP keep-alive handler")
		t, err := methods.GetCurrentTime(ctx, c)
		if err != nil {
			return err
		}

		logger.Infof("vCenter current time: %s", t.String())
		return nil
	}
}

func isNotAuthenticated(err error) bool {
	if soap.IsSoapFault(err) {
		switch soap.ToSoapFault(err).VimFault().(type) {
		case types.NotAuthenticated:
			return true
		}
	}
	return false
}
