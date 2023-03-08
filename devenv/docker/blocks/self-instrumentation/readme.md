# Self Instrumentation

To run this source, in the Grafana repo root:

```
make devenv sources=self-instrumentation
```

This will setup Prometheus, Loki and Tempo.

You then need to run Grafana with those added config:

```ini
[log.frontend]
provider = grafana
custom_endpoint = http://localhost:12347/collect
api_key = api_key

[tracing.opentelemetry.jaeger]
address = http://localhost:14268/api/traces
```

## Metrics

### Backend - Go

Go code use the [prometheus client](https://github.com/prometheus/client_golang) to expose metrics via the endpoint `/metrics`.
If you run Grafana localy on http://localhost:3000, then http://localhost:3000/metrics will expose the metrics. This endpoint can be scraped
by Prometheus.

Here is a Prometheus config example:

```yaml
global:
  scrape_interval:     15s
  evaluation_interval: 15s

scrape_configs:
  - job_name: grafana
    static_configs:
      - targets:
        - host.docker.internal:3000
```

Custom Metrics are (almost) all prefixed with `grafana_`.

Here is how we could create a new custom metric that track the number of time the endpoint `/api/user` has been called:

In the file `pkg/api/user.go`, update the first handler to look like this:

```go
import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var apiUserTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: "grafana",
	Name:      "my_custom_api_user_total",
	Help:      "The total amount of call to /api/user",
}, []string{"plugin_id", "endpoint", "status", "target"})

// swagger:route GET /user signed_in_user getSignedInUser
//
// Get (current authenticated user)
//
// Responses:
// 200: userResponse
// 401: unauthorisedError
// 403: forbiddenError
// 404: notFoundError
// 500: internalServerError
func (hs *HTTPServer) GetSignedInUser(c *contextmodel.ReqContext) response.Response {
	apiUserTotal.Inc();

	return hs.getUserUserProfile(c, c.UserID)
}
```


You can now visit your [profile](http://localhost:3000/profile) page a few times and run a query on the Prometheus datasource to see your custom metric:

http://localhost:3000/explore?orgId=1&left=%7B%22datasource%22:%22e34584fb-98e4-4d5c-83ae-3c0000dea48a%22,%22queries%22:%5B%7B%22refId%22:%22A%22,%22expr%22:%22grafana_my_custom_api_user_total%7B%7D%22,%22range%22:true,%22datasource%22:%7B%22type%22:%22prometheus%22,%22uid%22:%22e34584fb-98e4-4d5c-83ae-3c0000dea48a%22%7D,%22editorMode%22:%22code%22%7D%5D,%22range%22:%7B%22from%22:%22now-5m%22,%22to%22:%22now%22%7D%7D