package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/benbjohnson/clock"
	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
	"go.uber.org/zap/zaptest"
	"gotest.tools/assert"
	"knative.dev/pkg/logging"
)

const (
	vc        = "https://vcenter.corp.local/sdk"
	suffix    = "AlarmInfo"
	injectKey = "AlarmInfo"
)

func Test_validateEnv(t *testing.T) {
	type args struct {
		env envConfig
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "invalid TTL",
			args: args{
				env: envConfig{
					TTL:         -10,
					EventSuffix: "AlarmInfo",
					InjectKey:   "AlarmInfo",
				}},
			wantErr: true,
		},
		{
			name: "invalid suffix",
			args: args{
				env: envConfig{
					TTL:         60,
					EventSuffix: ".AlarmInfo",
					InjectKey:   "AlarmInfo",
				}},
			wantErr: true,
		},
		{
			name: "invalid key",
			args: args{
				env: envConfig{
					TTL:         60,
					EventSuffix: "AlarmInfo",
					InjectKey:   "Alarm-Info",
				}},
			wantErr: true,
		},
		{
			// only doing semantic verification since envconfig will do the heavy lifting
			name: "valid env",
			args: args{
				env: envConfig{
					TTL:         10,
					EventSuffix: "enriched",
					InjectKey:   "AlarmKey",
				}},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateEnv(tt.args.env); (err != nil) != tt.wantErr {
				t.Errorf("validateEnv() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_injectInfo(t *testing.T) {
	testEvents := createCloudEvents(t)

	invalidEvent := cloudevents.NewEvent()
	invalidData := []byte("invalid")
	err := invalidEvent.SetData(cloudevents.ApplicationJSON, invalidData)
	assert.NilError(t, err)

	type args struct {
		event cloudevents.Event
		key   string
		info  types.AlarmInfo
	}
	tests := []struct {
		name    string
		args    args
		want    []byte
		wantErr bool
	}{
		{
			name: "invalid event data",
			args: args{
				event: invalidEvent,
				key:   injectKey,
				info:  createAlarm(t, "alarm-1").Info,
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "AlarmStatusChangedEvent",
			args: args{
				event: *testEvents["AlarmStatusChangedEvent"],
				key:   injectKey,
				info:  createAlarm(t, "alarm-1").Info,
			},
			want:    patchedEvent,
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := injectInfo(tt.args.event, tt.args.key, tt.args.info)
			if (err != nil) != tt.wantErr {
				t.Errorf("injectInfo() error = %v, wantErr %v", err, tt.wantErr)
			}

			assert.DeepEqual(t, got, tt.want)
		})
	}
}

func Test_alarmServer_handleEvent(t *testing.T) {
	testEvents := createCloudEvents(t)

	type fields struct {
		cache *cache
	}
	type args struct {
		event cloudevents.Event
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   *cloudevents.Event
	}{
		{
			name: "event is not AlarmEvent",
			fields: fields{
				cache: nil,
			},
			args: args{
				event: *testEvents["VmPoweredOnEvent"],
			},
			want: nil,
		},
		{
			name: "ignore own (injected) event",
			fields: fields{
				cache: nil,
			},
			args: args{
				event: *testEvents["AlarmStatusChangedEvent.AlarmInfo"],
			},
			want: nil,
		},
		{
			name: "event is AlarmStatusChangedEvent",
			fields: fields{
				cache: &cache{
					clock: clock.NewMock(),
					ttl:   3600,
					// fill cache because vscim does not support alarms
					cache: map[string]*item{
						"Alarm:alarm-1": {
							alarm: createAlarm(t, "alarm-1"),
						}},
				},
			},
			args: args{
				event: *testEvents["AlarmStatusChangedEvent"],
			},
			want: testEvents["AlarmStatusChangedEvent."+suffix],
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &alarmServer{
				ceClient:  nil,
				cache:     tt.fields.cache,
				source:    vc,
				suffix:    "." + suffix,
				injectKey: injectKey,
			}

			logger := zaptest.NewLogger(t).Sugar()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ctx = logging.WithLogger(ctx, logger)

			got := a.handleEvent(ctx, tt.args.event)
			assert.DeepEqual(t, got, tt.want)
		})
	}
}

func createAlarm(t *testing.T, name string) mo.Alarm {
	t.Helper()
	return mo.Alarm{
		Info: types.AlarmInfo{
			AlarmSpec: types.AlarmSpec{
				Name:        name,
				Description: "A test alarm",
				Enabled:     true,
			},
			Alarm: types.ManagedObjectReference{
				Type:  "Alarm",
				Value: name,
			},
		},
	}
}

func createCloudEvents(t *testing.T) map[string]*cloudevents.Event {
	t.Helper()

	eventMap := map[string]*cloudevents.Event{}

	// VmPoweredOnEvent
	vmEvent := types.VmEvent{
		Event: types.Event{
			Key: 1,
			Vm: &types.VmEventArgument{
				Vm: types.ManagedObjectReference{
					Type:  "VirtualMachine",
					Value: "vm-1",
				},
			},
		},
	}

	ceVmEvent := cloudevents.NewEvent()
	ceVmEvent.SetSource(vc)
	ceVmEvent.SetType("VmPoweredOnEvent")

	vmData, err := json.Marshal(vmEvent)
	assert.NilError(t, err)
	err = ceVmEvent.SetData(cloudevents.ApplicationJSON, vmData)
	assert.NilError(t, err)

	eventMap["VmPoweredOnEvent"] = &ceVmEvent

	// AlarmStatusChangedEvent
	alarmEvent := types.AlarmStatusChangedEvent{
		AlarmEvent: types.AlarmEvent{
			Event: types.Event{
				Key: 1,
			},
			Alarm: types.AlarmEventArgument{
				Alarm: types.ManagedObjectReference{
					Type:  "Alarm",
					Value: "alarm-1",
				},
			},
		},
		From: "green",
		To:   "yellow",
	}

	ceAlarmEvent := cloudevents.NewEvent()
	ceAlarmEvent.SetSource(vc)
	ceAlarmEvent.SetType("AlarmStatusChangedEvent")

	alarmData, err := json.Marshal(alarmEvent)
	assert.NilError(t, err)
	err = ceAlarmEvent.SetData(cloudevents.ApplicationJSON, alarmData)
	assert.NilError(t, err)

	eventMap["AlarmStatusChangedEvent"] = &ceAlarmEvent

	// AlarmStatusChangedEvent.AlarmInfo
	ceAlarmEventInjected := ceAlarmEvent.Clone()
	ceAlarmEventInjected.SetType("AlarmStatusChangedEvent." + suffix)
	err = ceAlarmEventInjected.SetData(cloudevents.ApplicationJSON, patchedEvent)
	assert.NilError(t, err)

	eventMap["AlarmStatusChangedEvent."+suffix] = &ceAlarmEventInjected

	return eventMap
}

var patchedEvent = []byte(`{"Alarm":{"Alarm":{"Type":"Alarm","Value":"alarm-1"},"Name":""},"AlarmInfo":{"Name":"alarm-1","SystemName":"","Description":"A test alarm","Enabled":true,"Expression":null,"Action":null,"ActionFrequency":0,"Setting":null,"Key":"","Alarm":{"Type":"Alarm","Value":"alarm-1"},"Entity":{"Type":"","Value":""},"LastModifiedTime":"0001-01-01T00:00:00Z","LastModifiedUser":"","CreationEventId":0},"ChainId":0,"ChangeTag":"","ComputeResource":null,"CreatedTime":"0001-01-01T00:00:00Z","Datacenter":null,"Ds":null,"Dvs":null,"Entity":{"Entity":{"Type":"","Value":""},"Name":""},"From":"green","FullFormattedMessage":"","Host":null,"Key":1,"Net":null,"Source":{"Entity":{"Type":"","Value":""},"Name":""},"To":"yellow","UserName":"","Vm":null}`)
