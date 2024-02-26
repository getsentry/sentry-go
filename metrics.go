package sentry

import "math"

type (
	MetricUnit int

	NumberOrString interface {
		int | string
	}

	void struct{}
)

var (
	member void
)

const (
	// Duration units
	NanoSecond MetricUnit = iota
	MicroSecond
	MilliSecond
	Second
	Minute
	Hour
	Day
	Week
	// Information units
	Bit
	Byte
	KiloByte
	KibiByte
	MegaByte
	MebiByte
	GigaByte
	GibiByte
	TeraByte
	TebiByte
	PetaByte
	PebiByte
	ExaByte
	ExbiByte
	// Fractions units
	Ratio
	Percent
)

type Metric interface {
	GetType() string
	GetTags() map[string]string
	GetKey() string
	GetUnit() string
	GetTimestamp() int
}

func (m MetricUnit) toString() string {
	switch m {
	case NanoSecond:
		return "nanosecond"
	case MicroSecond:
		return "microsecond"
	case MilliSecond:
		return "millisecond"
	case Second:
		return "second"
	case Minute:
		return "minute"
	case Hour:
		return "hour"
	case Day:
		return "day"
	case Week:
		return "week"
	case Bit:
		return "bit"
	case Byte:
		return "byte"
	case KiloByte:
		return "kilobyte"
	case KibiByte:
		return "kibibyte"
	case MegaByte:
		return "megabyte"
	case MebiByte:
		return "mebibyte"
	case GigaByte:
		return "gigabyte"
	case GibiByte:
		return "gibibyte"
	case TeraByte:
		return "terabyte"
	case TebiByte:
		return "tebibyte"
	case PetaByte:
		return "petabyte"
	case PebiByte:
		return "pebibyte"
	case ExaByte:
		return "exabyte"
	case ExbiByte:
		return "exbibyte"
	case Ratio:
		return "ratio"
	case Percent:
		return "percent"
	default:
		return "error"
	}
}

type abstractMetric struct {
	key       string
	unit      MetricUnit
	tags      map[string]string
	timestamp int
}

func (am abstractMetric) GetTags() map[string]string {
	return am.tags
}

func (am abstractMetric) GetKey() string {
	return am.key
}

func (am abstractMetric) GetUnit() string {
	return am.unit.toString()
}

func (am abstractMetric) GetTimestamp() int {
	return am.timestamp
}

// ////////////////////////
// Counter Metric
// ////////////////////////
type CounterMetric struct {
	value float64
	abstractMetric
}

func (c *CounterMetric) Add(value float64) {
	c.value += value
}

func (c CounterMetric) GetType() string {
	return "c"
}

func NewCounterMetric(key string, unit MetricUnit, tags map[string]string, timestamp int, value float64) CounterMetric {
	am := abstractMetric{
		key,
		unit,
		tags,
		timestamp,
	}

	return CounterMetric{
		value,
		am,
	}
}

// ////////////////////////
// Distribution Metric
// ////////////////////////
type DistributionMetric struct {
	values []float64
	abstractMetric
}

func (d *DistributionMetric) Add(value float64) {
	d.values = append(d.values, value)
}

func (d DistributionMetric) GetType() string {
	return "d"
}

func NewDistributionMetric(key string, unit MetricUnit, tags map[string]string, timestamp int, value float64) DistributionMetric {
	am := abstractMetric{
		key,
		unit,
		tags,
		timestamp,
	}

	return DistributionMetric{
		[]float64{value},
		am,
	}
}

// ////////////////////////
// Gauge Metric
// ////////////////////////
type GaugeMetric struct {
	last  float64
	min   float64
	max   float64
	sum   float64
	count float64
	abstractMetric
}

func (g *GaugeMetric) Add(value float64) {
	g.last = value
	g.min = math.Min(g.min, value)
	g.max = math.Max(g.max, value)
	g.sum = g.sum + value
	g.count += 1
}

func (g GaugeMetric) GetType() string {
	return "g"
}

func NewGaugeMetric(key string, unit MetricUnit, tags map[string]string, timestamp int, value float64) GaugeMetric {
	am := abstractMetric{
		key,
		unit,
		tags,
		timestamp,
	}

	return GaugeMetric{
		value, // last
		value, // min
		value, // max
		value, // sum
		value, // count
		am,
	}
}

// ////////////////////////
// Set Metric
// ////////////////////////
type SetMetric[T NumberOrString] struct {
	values map[T]void
	abstractMetric
}

func (s *SetMetric[T]) Add(value T) {
	s.values[value] = member
}

func (s SetMetric[T]) GetType() string {
	return "s"
}

func NewSetMetric[T NumberOrString](key string, unit MetricUnit, tags map[string]string, timestamp int, value T) SetMetric[T] {
	am := abstractMetric{
		key,
		unit,
		tags,
		timestamp,
	}

	return SetMetric[T]{
		values: map[T]void{
			value: member,
		},
		abstractMetric: am,
	}
}
