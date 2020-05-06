package reliable

type ConnOption interface {
	applyConn(c *Conn)
}

type EndpointOption interface {
	applyEndpoint(e *Endpoint)
}

type Option interface {
	ConnOption
	EndpointOption
}

type withBufferPool struct{ pool *Pool }

func (o withBufferPool) applyConn(c *Conn)         { c.pool = o.pool }
func (o withBufferPool) applyEndpoint(e *Endpoint) { e.pool = o.pool }

func WithBufferPool(pool *Pool) Option { return withBufferPool{pool: pool} }

type withWriteBufferSize struct{ writeBufferSize uint16 }

func (o withWriteBufferSize) applyConn(c *Conn)         { c.writeBufferSize = o.writeBufferSize }
func (o withWriteBufferSize) applyEndpoint(e *Endpoint) { e.writeBufferSize = o.writeBufferSize }

func WithWriteBufferSize(writeBufferSize uint16) Option {
	if 65536%uint32(writeBufferSize) != 0 {
		panic("write buffer size must be smaller than 65536 and a power of two")
	}
	return withWriteBufferSize{writeBufferSize: writeBufferSize}
}

type withReadBufferSize struct{ readBufferSize uint16 }

func (o withReadBufferSize) applyConn(c *Conn)         { c.readBufferSize = o.readBufferSize }
func (o withReadBufferSize) applyEndpoint(e *Endpoint) { e.readBufferSize = o.readBufferSize }

func WithReadBufferSize(readBufferSize uint16) Option {
	if 65536%uint32(readBufferSize) != 0 {
		panic("read buffer size must be smaller than 65536 and a power of two")
	}
	return withReadBufferSize{readBufferSize: readBufferSize}
}

type withHandler struct{ handler Handler }

func (o withHandler) applyConn(c *Conn)         { c.handler = o.handler }
func (o withHandler) applyEndpoint(e *Endpoint) { e.handler = o.handler }

func WithHandler(handler Handler) Option { return withHandler{handler: handler} }
