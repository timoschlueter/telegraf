package librelinkup

import (
	"github.com/influxdata/telegraf/testutil"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestGather(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.RequestURI == "/llu/auth/login" {
			data, err := os.ReadFile("testdata/session.json")
			require.NoErrorf(t, err, "could not read mock json data")
			w.WriteHeader(http.StatusOK)
			_, err = w.Write(data)
			require.NoError(t, err)
		}

		if r.RequestURI == "/llu/connections" {
			data, err := os.ReadFile("testdata/connections.json")
			require.NoErrorf(t, err, "could not read mock json data")
			w.WriteHeader(http.StatusOK)
			_, err = w.Write(data)
			require.NoError(t, err)
		}

		if r.RequestURI == "/llu/connections/639dac0c-7065-4488-a782-ef81905213f3/graph" {
			data, err := os.ReadFile("testdata/measurements.json")
			require.NoErrorf(t, err, "could not read mock json data")
			w.WriteHeader(http.StatusOK)
			_, err = w.Write(data)
			require.NoError(t, err)
		}
	}))
	defer ts.Close()

	client := &http.Client{
		Timeout: 15 * time.Second,
	}

	acc := &testutil.Accumulator{}
	l := &LibreLinkUp{
		client:    client,
		PatientId: "639dac0c-7065-4488-a782-ef81905213f3",
		Version:   "4.2.2",
		Product:   "llu.ios"}
	l.ApiUrl = ts.URL

	require.NoError(t, l.Gather(acc))

	tags := map[string]string{
		"patient_id": "639dac0c-7065-4488-a782-ef81905213f3",
		"sensor_sn":  "123ABCD456",
	}

	fields := map[string]interface{}{
		"mg_dl":     222,
		"mmol_l":    float32(12.321),
		"timestamp": "2022-08-29 05:45:13 +0000 UTC",
	}

	acc.AssertContainsTaggedFields(t, "librelinkup", fields, tags)
}
