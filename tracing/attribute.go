package sentrytracing

type AttributeValue interface {
	~string | ~bool | ~int64 | ~float64
}

type Attribute[T AttributeValue] struct {
	Key   string
	Value T
}

type AttributeBuilder interface {
	string(key string, value string) AttributeBuilder
	boolean(key string, value bool) AttributeBuilder
	integer(key string, value int64) AttributeBuilder
	float(key string, value float64) AttributeBuilder
}

type attributeBuilder struct {
	attr any
}

func (b *attributeBuilder) string(key string, value string) AttributeBuilder {
	b.attr = Attribute[string]{Key: key, Value: value}
	return b
}

func (b *attributeBuilder) boolean(key string, value bool) AttributeBuilder {
	b.attr = Attribute[bool]{Key: key, Value: value}
	return b
}

func (b *attributeBuilder) integer(key string, value int64) AttributeBuilder {
	b.attr = Attribute[int64]{Key: key, Value: value}
	return b
}

func (b *attributeBuilder) float(key string, value float64) AttributeBuilder {
	b.attr = Attribute[float64]{Key: key, Value: value}
	return b
}

func String(key string, value string) AttributeBuilder {
	b := &attributeBuilder{}
	return b.string(key, value)
}

func Boolean(key string, value bool) AttributeBuilder {
	b := &attributeBuilder{}
	return b.boolean(key, value)
}

func Integer(key string, value int64) AttributeBuilder {
	b := &attributeBuilder{}
	return b.integer(key, value)
}

func Float(key string, value float64) AttributeBuilder {
	b := &attributeBuilder{}
	return b.float(key, value)
}
