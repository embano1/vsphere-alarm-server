# About

Blurp eventing, CloudEvents, Cache, Alarm enricher

# Usage 

```
kubectl apply -f ...

```

```
./vsphere-alarm-server -h
Usage of ./vsphere-alarm-server:
  -v    print version information
```

## Environment Variables

```
"PORT" default:"8080" required:"true"`
"CACHE_TTL" default:"3600"`
"VCENTER_URL" default:"" required:"true"`
"VCENTER_INSECURE" default:"false"`
"SECRET_PATH" default:"/var/bindings/vsphere" required:"true"`
"DEBUG" default:"false"`
"EVENT_SUFFIX" default:"" required:"true"`
"ALARM_KEY" default:"" required:"true"`
```

