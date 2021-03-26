package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	v2 "github.com/cloudevents/sdk-go/v2"
	"github.com/cloudevents/sdk-go/v2/client"
	"github.com/kelseyhightower/envconfig"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
	"knative.dev/pkg/logging"
)

const (
	envPrefix        = ""
	defaultMountPath = "/var/bindings/vsphere" // filepath.Join isn't const.
)

type envConfig struct {
	Port        int    `envconfig:"PORT" default:"8080" required:"true"`
	TTL         int64  `envconfig:"CACHE_TTL" default:"3600"`
	VCenter     string `envconfig:"VCENTER_URL" default:"" required:"true"`
	Insecure    bool   `envconfig:"VCENTER_INSECURE" default:"false"`
	SecretPath  string `envconfig:"SECRET_PATH" default:"/var/bindings/vsphere" required:"true"`
	Debug       bool   `envconfig:"DEBUG" default:"false"`
	EventSuffix string `envconfig:"EVENT_SUFFIX" default:"" required:"true"`
	InjectKey   string `envconfig:"ALARM_KEY" default:"" required:"true"`
}

type alarmServer struct {
	vcClient  *govmomi.Client
	ceClient  client.Client
	cache     *cache
	errCh     chan error
	source    string
	suffix    string
	injectKey string
}

func newAlarmServer(ctx context.Context) (*alarmServer, error) {
	var env envConfig
	if err := envconfig.Process(envPrefix, &env); err != nil {
		return nil, fmt.Errorf("process env var: %w", err)
	}

	if err := validateEnv(env); err != nil {
		return nil, err
	}

	vc, err := newSOAPClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("create vsphere client: %w", err)
	}

	p, err := v2.NewHTTP(v2.WithPort(env.Port))
	if err != nil {
		return nil, fmt.Errorf("create cloudevents transport: %w", err)
	}

	ce, err := v2.NewClient(p, v2.WithTimeNow(), v2.WithUUIDs())
	if err != nil {
		return nil, fmt.Errorf("create cloudevents client, %w", err)
	}

	a := alarmServer{
		vcClient:  vc,
		ceClient:  ce,
		cache:     newAlarmCache(env.TTL),
		errCh:     make(chan error, 1), // any error received will lead to termination
		source:    vc.URL().String(),
		suffix:    fmt.Sprintf(".%s", env.EventSuffix),
		injectKey: env.InjectKey,
	}

	return &a, nil
}

func (a *alarmServer) handleEvent(ctx context.Context, event v2.Event) *v2.Event {
	logger := logging.FromContext(ctx)

	if event.Source() == a.source && strings.Contains(event.Type(), a.suffix) {
		logger.Debugw("ignoring own event", "id", event.ID(), "source", event.Source(), "type", event.Type())
		return nil
	}

	// TODO: only JSON encoding supported
	if !bytes.HasPrefix(event.Data(), []byte("{")) {
		logger.Warnw("ignoring event: data is not JSON-encoded", "id", event.ID(), "type", event.Type(), "content-type", event.DataContentType())
		return nil
	}

	var alarmEvent types.AlarmEvent
	if err := event.DataAs(&alarmEvent); err != nil {
		logger.Warnw("decode vcenter event: %v", err)
		return nil
	}

	// additional check to verify it's an alarm event because decoding above might
	// succeed even in case of non alarm event due to embedded Event object
	if moref := alarmEvent.Alarm.Alarm; moref.Type != "" {
		logger.Infow("got alarm event", "source", a.source, "type", event.Type(), "moref", moref.String())

		var (
			alarm mo.Alarm
			found bool
		)

		if alarm, found = a.cache.get(moref.String()); !found {
			pc := property.DefaultCollector(a.vcClient.Client)
			if err := pc.RetrieveOne(ctx, moref, nil, &alarm); err != nil {
				if isNotAuthenticated(err) {
					// TODO: easy way terminate and restart
					a.errCh <- fmt.Errorf("vsphere session not authenticated: %w", err)
					return nil
				}
				logger.Errorf("retrieve alarm from vcenter: %v", err)
				return nil
			}
			logger.Debugf("retrieved alarm details from vcenter: %v", alarm.Info)
			logger.Debugf("adding %s to cache", moref.String())
			a.cache.add(moref.String(), alarm)
		} else {
			logger.Debugf("retrieved alarm details from cache: %v", alarm.Info)
		}

		resp := cloudevents.NewEvent()
		resp.SetSource(a.source)
		resp.SetType(event.Type() + a.suffix)

		var origEvent map[string]interface{}
		err := event.DataAs(&origEvent)
		if err != nil {
			logger.Warnf("decode original event: %v", err)
			return nil
		}

		// inject info into original event
		origEvent[a.injectKey] = alarm.Info

		b, err := json.Marshal(origEvent)
		if err != nil {
			logger.Errorf("marshal cloud event response data: %v", err)
			return nil
		}

		err = resp.SetData(cloudevents.ApplicationJSON, b)
		if err != nil {
			logger.Errorf("set cloud event response data: %v", err)
			return nil
		}
		logger.Debugw("returning enriched alarm event", "id", resp.ID(), "source", resp.Source(), "type", resp.Type())
		return &resp
	}

	logger.Debugf("ignoring event: not an AlarmEvent: %s", string(event.Data()))
	return nil
}

func validateEnv(env envConfig) error {
	validKey := func(s string) bool {
		for _, r := range s {
			if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
				return false
			}
		}
		return true
	}

	if !validKey(env.InjectKey) {
		return fmt.Errorf("ALARM_KEY contains non-letter characters: %s", env.InjectKey)
	}

	if env.TTL < 0 {
		return fmt.Errorf("CACHE_TTL must be greater than 0: %d", env.TTL)
	}

	if strings.HasPrefix(env.EventSuffix, ".") {
		return fmt.Errorf("EVEN_SUFFIX must not start with %q: %s", env.EventSuffix, ".")
	}

	return nil
}
