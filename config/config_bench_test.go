package config

import (
	"os"
	"reflect"
	"strconv"
	"testing"
	"time"
)

// Prepare env vars for benchmark
func init() {
	_ = os.Setenv("PGRWL_MODE", "receive")
	_ = os.Setenv("PGRWL_DIRECTORY", "/tmp/test")
	_ = os.Setenv("PGRWL_SLOT", "slot")
	_ = os.Setenv("PGRWL_NO_LOOP", "true")
	_ = os.Setenv("PGRWL_LISTEN_PORT", "5432")
	_ = os.Setenv("PGRWL_LOG_LEVEL", "debug")
	_ = os.Setenv("PGRWL_LOG_FORMAT", "json")
	_ = os.Setenv("PGRWL_LOG_ADD_SOURCE", "true")
	_ = os.Setenv("PGRWL_S3_URL", "http://s3")
	_ = os.Setenv("PGRWL_S3_ACCESS_KEY_ID", "admin")
	_ = os.Setenv("PGRWL_S3_SECRET_ACCESS_KEY", "secret")
	_ = os.Setenv("PGRWL_S3_BUCKET", "backups")
	_ = os.Setenv("PGRWL_S3_REGION", "us-west")
	_ = os.Setenv("PGRWL_S3_USE_PATH_STYLE", "true")
	_ = os.Setenv("PGRWL_S3_DISABLE_SSL", "true")
}

// go test -bench . github.com/hashmap-kz/pgrwl/config -benchmem

func BenchmarkDirectSet(b *testing.B) {
	for i := 0; i < b.N; i++ {
		cfg := &Config{}
		mergeEnvIfUnset(cfg, DefaultValues)
	}
}

func BenchmarkReflectSet(b *testing.B) {
	for i := 0; i < b.N; i++ {
		cfg := &Config{}
		fillUnsetFieldsFromEnv(cfg)
	}
}

// using reflection

func fillUnsetFieldsFromEnv(cfg any) {
	v := reflect.ValueOf(cfg).Elem()
	t := v.Type()

	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		structField := t.Field(i)
		jsonKey := structField.Tag.Get("json")

		// Skip if no json tag or if already set (non-zero)
		if jsonKey == "" || !isZero(field) {
			continue
		}

		envVal, ok := os.LookupEnv(jsonKey)
		val := envVal
		if !ok || val == "" {
			val = structField.Tag.Get("envDefault")
			if val == "" {
				continue // no env, no default — skip
			}
		}

		// Assign value based on kind
		switch field.Kind() {
		case reflect.String:
			field.SetString(envVal)
		case reflect.Bool:
			if b, err := strconv.ParseBool(envVal); err == nil {
				field.SetBool(b)
			}
		case reflect.Int:
			if i, err := strconv.Atoi(envVal); err == nil {
				field.SetInt(int64(i))
			}
		default:
		}

		// Special case: time.Duration
		if field.Type() == reflect.TypeOf(time.Duration(0)) {
			if d, err := time.ParseDuration(envVal); err == nil {
				field.Set(reflect.ValueOf(d))
			}
		}
	}
}

func isZero(v reflect.Value) bool {
	return v.IsZero()
}
