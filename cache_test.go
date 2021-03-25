package main

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/benbjohnson/clock"
	"github.com/vmware/govmomi/vim25/mo"
	"go.uber.org/zap/zaptest"
	"knative.dev/pkg/logging"
)

func Test_cache_get(t *testing.T) {
	type fields struct {
		clock clock.Clock
		ttl   int64
		cache map[string]*item
	}
	type args struct {
		key string
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   mo.Alarm
		found  bool
	}{
		{
			name: "get item with TTL greater than next GC interval",
			fields: fields{
				clock: clock.NewMock(),
				ttl:   60,
				cache: map[string]*item{},
			},
			args:  args{"alarm-1"},
			want:  mo.Alarm{},
			found: true,
		},
		{
			name: "get item with TTL smaller than next GC interval",
			fields: fields{
				clock: clock.NewMock(),
				ttl:   1,
				cache: map[string]*item{},
			},
			args:  args{"alarm-1"},
			want:  mo.Alarm{},
			found: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &cache{
				clock: tt.fields.clock,
				ttl:   tt.fields.ttl,
				cache: tt.fields.cache,
			}
			a.add(tt.args.key, mo.Alarm{})

			logger := zaptest.NewLogger(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ctx = logging.WithLogger(ctx, logger.Sugar())

			go func() {
				// advance clock by 10s
				a.clock.(*clock.Mock).Add(time.Second * 20)

				got, found := a.get(tt.args.key)
				if !reflect.DeepEqual(got, tt.want) {
					t.Errorf("get() got = %v, want %v", got, tt.want)
				}
				if found != tt.found {
					t.Errorf("get() got1 = %v, want %v", found, tt.found)
				}
				cancel()
			}()

			if err := a.run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				t.Fatalf("run cache: %v", err)
			}
		})
	}
}
