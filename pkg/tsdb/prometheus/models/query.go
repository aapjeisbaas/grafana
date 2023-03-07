package models

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana/pkg/tsdb/intervalv2"
	"github.com/grafana/grafana/pkg/tsdb/prometheus/kinds/dataquery"
)

// Internal interval and range variables
const (
	varInterval     = "$__interval"
	varIntervalMs   = "$__interval_ms"
	varRange        = "$__range"
	varRangeS       = "$__range_s"
	varRangeMs      = "$__range_ms"
	varRateInterval = "$__rate_interval"
)

// Internal interval and range variables with {} syntax
// Repetitive code, we should have functionality to unify these
const (
	varIntervalAlt     = "${__interval}"
	varIntervalMsAlt   = "${__interval_ms}"
	varRangeAlt        = "${__range}"
	varRangeSAlt       = "${__range_s}"
	varRangeMsAlt      = "${__range_ms}"
	varRateIntervalAlt = "${__rate_interval}"
)

type TimeSeriesQueryType string

const (
	RangeQueryType    TimeSeriesQueryType = "range"
	InstantQueryType  TimeSeriesQueryType = "instant"
	ExemplarQueryType TimeSeriesQueryType = "exemplar"
	UnknownQueryType  TimeSeriesQueryType = "unknown"
)

var safeResolution = 11000

type QueryModel struct {
	dataquery.PrometheusDataQuery
	// Timezone offset to align start & end time on backend
	UtcOffsetSec   int64  `json:"utcOffsetSec,omitempty"`
	LegendFormat   string `json:"legendFormat,omitempty"`
	RequestId      string `json:"requestId,omitempty"`
	ValueWithRefId bool   `json:"valueWithRefId,omitempty"`
}

type TimeRange struct {
	Start time.Time
	End   time.Time
	Step  time.Duration
}

type Query struct {
	Expr          string
	Step          time.Duration
	LegendFormat  string
	Start         time.Time
	End           time.Time
	RefId         string
	InstantQuery  bool
	RangeQuery    bool
	ExemplarQuery bool
	UtcOffsetSec  int64
}

func Parse(query backend.DataQuery, timeInterval string, intervalCalculator intervalv2.Calculator, fromAlert bool) (*Query, error) {
	model := &QueryModel{}
	if err := json.Unmarshal(query.JSON, model); err != nil {
		return nil, err
	}

	queryInterval := ""
	if model.Interval != nil {
		queryInterval = *model.Interval
	}

	queryIntervalMs := int64(0)
	if model.IntervalMs != nil {
		queryIntervalMs = *model.IntervalMs
	}

	queryIntervalFactor := int64(1)
	if model.IntervalFactor != nil {
		queryIntervalFactor = *model.IntervalFactor
	}

	// Final interval value
	interval, err := calculatePrometheusInterval(queryInterval, timeInterval, queryIntervalMs,
		queryIntervalFactor, query,
		intervalCalculator)
	if err != nil {
		return nil, err
	}

	// Interpolate variables in expr
	timeRange := query.TimeRange.To.Sub(query.TimeRange.From)
	expr := interpolateVariables(model.Expr, queryInterval, interval, timeRange, intervalCalculator, timeInterval)
	var rangeQuery, instantQuery bool
	if model.Instant == nil {
		instantQuery = false
	} else {
		instantQuery = *model.Instant
	}
	if model.Range == nil {
		rangeQuery = false
	} else {
		rangeQuery = *model.Range
	}
	if !instantQuery && !rangeQuery {
		// In older dashboards, we were not setting range query param and !range && !instant was run as range query
		rangeQuery = true
	}

	// We never want to run exemplar query for alerting
	exemplarQuery := false
	if model.Exemplar != nil {
		exemplarQuery = *model.Exemplar
	}
	if fromAlert {
		exemplarQuery = false
	}

	return &Query{
		Expr:          expr,
		Step:          interval,
		LegendFormat:  model.LegendFormat,
		Start:         query.TimeRange.From,
		End:           query.TimeRange.To,
		RefId:         query.RefID,
		InstantQuery:  instantQuery,
		RangeQuery:    rangeQuery,
		ExemplarQuery: exemplarQuery,
		UtcOffsetSec:  model.UtcOffsetSec,
	}, nil
}

func (query *Query) Type() TimeSeriesQueryType {
	if query.InstantQuery {
		return InstantQueryType
	}
	if query.RangeQuery {
		return RangeQueryType
	}
	if query.ExemplarQuery {
		return ExemplarQueryType
	}
	return UnknownQueryType
}

func (query *Query) TimeRange() TimeRange {
	return TimeRange{
		Step: query.Step,
		// Align query range to step. It rounds start and end down to a multiple of step.
		Start: AlignTimeRange(query.Start, query.Step, query.UtcOffsetSec),
		End:   AlignTimeRange(query.End, query.Step, query.UtcOffsetSec),
	}
}

func calculatePrometheusInterval(queryInterval, timeInterval string, intervalMs, intervalFactor int64,
	query backend.DataQuery, intervalCalculator intervalv2.Calculator) (time.Duration, error) {
	// If we are using variable for interval/step, we will replace it with calculated interval
	if isVariableInterval(queryInterval) {
		queryInterval = ""
	}

	minInterval, err := intervalv2.GetIntervalFrom(timeInterval, queryInterval, intervalMs, 15*time.Second)
	if err != nil {
		return time.Duration(0), err
	}
	calculatedInterval := intervalCalculator.Calculate(query.TimeRange, minInterval, query.MaxDataPoints)
	safeInterval := intervalCalculator.CalculateSafeInterval(query.TimeRange, int64(safeResolution))

	adjustedInterval := safeInterval.Value
	if calculatedInterval.Value > safeInterval.Value {
		adjustedInterval = calculatedInterval.Value
	}

	if queryInterval == varRateInterval || queryInterval == varRateIntervalAlt {
		// Rate interval is final and is not affected by resolution
		return calculateRateInterval(adjustedInterval, timeInterval, intervalCalculator), nil
	} else {
		queryIntervalFactor := intervalFactor
		if queryIntervalFactor == 0 {
			queryIntervalFactor = 1
		}
		return time.Duration(int64(adjustedInterval) * queryIntervalFactor), nil
	}
}

func calculateRateInterval(interval time.Duration, scrapeInterval string, intervalCalculator intervalv2.Calculator) time.Duration {
	scrape := scrapeInterval
	if scrape == "" {
		scrape = "15s"
	}

	scrapeIntervalDuration, err := intervalv2.ParseIntervalStringToTimeDuration(scrape)
	if err != nil {
		return time.Duration(0)
	}

	rateInterval := time.Duration(int64(math.Max(float64(interval+scrapeIntervalDuration), float64(4)*float64(scrapeIntervalDuration))))
	return rateInterval
}

func interpolateVariables(expr, queryInterval string, interval time.Duration,
	timeRange time.Duration,
	intervalCalculator intervalv2.Calculator, timeInterval string) string {
	rangeMs := timeRange.Milliseconds()
	rangeSRounded := int64(math.Round(float64(rangeMs) / 1000.0))

	var rateInterval time.Duration
	if queryInterval == varRateInterval || queryInterval == varRateIntervalAlt {
		rateInterval = interval
	} else {
		rateInterval = calculateRateInterval(interval, timeInterval, intervalCalculator)
	}

	expr = strings.ReplaceAll(expr, varIntervalMs, strconv.FormatInt(int64(interval/time.Millisecond), 10))
	expr = strings.ReplaceAll(expr, varInterval, intervalv2.FormatDuration(interval))
	expr = strings.ReplaceAll(expr, varRangeMs, strconv.FormatInt(rangeMs, 10))
	expr = strings.ReplaceAll(expr, varRangeS, strconv.FormatInt(rangeSRounded, 10))
	expr = strings.ReplaceAll(expr, varRange, strconv.FormatInt(rangeSRounded, 10)+"s")
	expr = strings.ReplaceAll(expr, varRateInterval, rateInterval.String())

	// Repetitive code, we should have functionality to unify these
	expr = strings.ReplaceAll(expr, varIntervalMsAlt, strconv.FormatInt(int64(interval/time.Millisecond), 10))
	expr = strings.ReplaceAll(expr, varIntervalAlt, intervalv2.FormatDuration(interval))
	expr = strings.ReplaceAll(expr, varRangeMsAlt, strconv.FormatInt(rangeMs, 10))
	expr = strings.ReplaceAll(expr, varRangeSAlt, strconv.FormatInt(rangeSRounded, 10))
	expr = strings.ReplaceAll(expr, varRangeAlt, strconv.FormatInt(rangeSRounded, 10)+"s")
	expr = strings.ReplaceAll(expr, varRateIntervalAlt, rateInterval.String())
	return expr
}

func isVariableInterval(interval string) bool {
	if interval == varInterval || interval == varIntervalMs || interval == varRateInterval {
		return true
	}
	// Repetitive code, we should have functionality to unify these
	if interval == varIntervalAlt || interval == varIntervalMsAlt || interval == varRateIntervalAlt {
		return true
	}
	return false
}

func AlignTimeRange(t time.Time, step time.Duration, offset int64) time.Time {
	offsetNano := float64(offset * 1e9)
	stepNano := float64(step.Nanoseconds())
	return time.Unix(0, int64(math.Floor((float64(t.UnixNano())+offsetNano)/stepNano)*stepNano-offsetNano)).UTC()
}
