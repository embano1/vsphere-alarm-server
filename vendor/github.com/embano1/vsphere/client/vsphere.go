package client

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"path/filepath"
	"time"

	"github.com/hashicorp/go-multierror"
	"github.com/kelseyhightower/envconfig"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/event"
	"github.com/vmware/govmomi/session"
	"github.com/vmware/govmomi/session/keepalive"
	"github.com/vmware/govmomi/task"
	"github.com/vmware/govmomi/vapi/rest"
	"github.com/vmware/govmomi/vapi/tags"
	"github.com/vmware/govmomi/vim25"
	"github.com/vmware/govmomi/vim25/methods"
	"github.com/vmware/govmomi/vim25/soap"
	"go.uber.org/zap"

	"github.com/embano1/vsphere/logger"
)

const (
	keepaliveInterval = 5 * time.Minute // vCenter APIs keep-alive
	userFileKey       = "username"
	passwordFileKey   = "password"
)

// Client is a combined vCenter SOAP and REST (VAPI) client with fields to
// directly access commonly used managers
type Client struct {
	SOAP   *govmomi.Client
	REST   *rest.Client
	Tags   *tags.Manager
	Tasks  *task.Manager
	Events *event.Manager
}

// Config configures the vsphere client via environment variables
type Config struct {
	Insecure   bool   `envconfig:"VCENTER_INSECURE" default:"false"`
	Address    string `envconfig:"VCENTER_URL" required:"true"`
	SecretPath string `envconfig:"VCENTER_SECRET_PATH" required:"true" default:"/var/bindings/vsphere"`
}

// readKey reads the file from the secret path
func readKey(key string) (string, error) {
	var env Config
	if err := envconfig.Process("", &env); err != nil {
		return "", err
	}

	data, err := ioutil.ReadFile(filepath.Join(env.SecretPath, key))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// New returns a combined vCenter SOAP and REST (VAPI) client with active
// keep-alive configured via environment variables. Commonly used managers are
// exposed for quick access.
//
// A custom logger (zap.Logger) can be injected into the context via the logger
// package.
//
// Use Logout() to release resources and perform a clean logout from vCenter.
func New(ctx context.Context) (*Client, error) {
	vclient, err := NewSOAP(ctx)
	if err != nil {
		return nil, fmt.Errorf("create vsphere SOAP client: %w", err)
	}

	rc, err := NewREST(ctx, vclient.Client)
	if err != nil {
		return nil, fmt.Errorf("create vsphere REST client: %w", err)
	}

	client := Client{
		SOAP:   vclient,
		REST:   rc,
		Tags:   tags.NewManager(rc),
		Tasks:  task.NewManager(vclient.Client),
		Events: event.NewManager(vclient.Client),
	}

	return &client, nil
}

// Logout attempts a clean logout from the various vCenter APIs
func (c *Client) Logout() error {
	ctx := context.Background()

	var result error
	if err := c.REST.Logout(ctx); err != nil {
		result = multierror.Append(result, err)
	}

	if err := c.SOAP.Logout(ctx); err != nil {
		result = multierror.Append(result, err)
	}

	return result
}

// NewSOAP returns a vCenter SOAP API client with active keep-alive
// configured via environment variables.
//
// Use Logout() to release resources and perform a clean logout from vCenter.
func NewSOAP(ctx context.Context) (*govmomi.Client, error) {
	var env Config
	if err := envconfig.Process("", &env); err != nil {
		return nil, err
	}

	parsedURL, err := soap.ParseURL(env.Address)
	if err != nil {
		return nil, err
	}

	// Read the username and password from the filesystem.
	username, err := readKey(userFileKey)
	if err != nil {
		return nil, err
	}
	password, err := readKey(passwordFileKey)
	if err != nil {
		return nil, err
	}
	parsedURL.User = url.UserPassword(username, password)

	return soapWithKeepalive(ctx, parsedURL, env.Insecure)
}

func soapWithKeepalive(ctx context.Context, url *url.URL, insecure bool) (*govmomi.Client, error) {
	sc := soap.NewClient(url, insecure)
	vc, err := vim25.NewClient(ctx, sc)
	if err != nil {
		return nil, err
	}
	vc.RoundTripper = keepalive.NewHandlerSOAP(sc, keepaliveInterval, soapKeepAliveHandler(ctx, vc))

	// explicitly create session to activate keep-alive handler via Login
	m := session.NewManager(vc)
	err = m.Login(ctx, url.User)
	if err != nil {
		return nil, err
	}

	c := govmomi.Client{
		Client:         vc,
		SessionManager: m,
	}

	return &c, nil
}

func soapKeepAliveHandler(ctx context.Context, c *vim25.Client) func() error {
	log := logger.Get(ctx)

	return func() error {
		log.Debug("executing SOAP keep-alive handler")
		t, err := methods.GetCurrentTime(ctx, c)
		if err != nil {
			log.Error("execute SOAP keep-alive handler", zap.Error(err))
			return err
		}

		log.Debug("vCenter current time", zap.String("time", t.String()))
		return nil
	}
}

// NewREST returns a vCenter REST (VAPI) API client with active keep-alive
// configured via environment variables.
//
// Use Logout() to release resources and perform a clean logout from vCenter.
func NewREST(ctx context.Context, vc *vim25.Client) (*rest.Client, error) {
	var env Config
	if err := envconfig.Process("", &env); err != nil {
		return nil, err
	}

	parsedURL, err := soap.ParseURL(env.Address)
	if err != nil {
		return nil, err
	}

	// Read the username and password from the filesystem.
	username, err := readKey(userFileKey)
	if err != nil {
		return nil, err
	}
	password, err := readKey(passwordFileKey)
	if err != nil {
		return nil, err
	}
	parsedURL.User = url.UserPassword(username, password)

	rc := rest.NewClient(vc)
	rc.Transport = keepalive.NewHandlerREST(rc, keepaliveInterval, restKeepAliveHandler(ctx, rc))

	// Login activates the keep-alive handler
	if err = rc.Login(ctx, parsedURL.User); err != nil {
		return nil, err
	}
	return rc, nil
}

func restKeepAliveHandler(ctx context.Context, restclient *rest.Client) func() error {
	log := logger.Get(ctx)

	return func() error {
		log.Debug("executing REST keep-alive handler")
		s, err := restclient.Session(ctx)
		if err != nil {
			// errors are not logged in govmomi keepalive handler
			log.Error("execute REST keep-alive handler", zap.Error(err))
			return err
		}
		if s != nil {
			return nil
		}
		log.Error("execute REST keep-alive handler", zap.Error(err))
		return errors.New(http.StatusText(http.StatusUnauthorized))
	}
}
