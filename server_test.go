package main

import (
	"testing"
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
			args: args{env: envConfig{
				TTL:         -10,
				EventSuffix: "AlarmInfo",
				InjectKey:   "AlarmInfo",
			}},
			wantErr: true,
		},
		{
			name: "invalid suffix",
			args: args{env: envConfig{
				TTL:         60,
				EventSuffix: ".AlarmInfo",
				InjectKey:   "AlarmInfo",
			}},
			wantErr: true,
		},
		{
			name: "invalid key",
			args: args{env: envConfig{
				TTL:         60,
				EventSuffix: "AlarmInfo",
				InjectKey:   "Alarm-Info",
			}},
			wantErr: true,
		},
		{
			// only doing semantic verification since envconfig will do the heavy lifting
			name: "valid env",
			args: args{env: envConfig{
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
