package reliable

import "time"

const (
	DefaultWriteBufferSize uint16 = 256
	DefaultReadBufferSize  uint16 = 256

	DefaultUpdatePeriod  = 100 * time.Millisecond
	DefaultResendTimeout = 100 * time.Millisecond
)

type ProtocolOption interface {
	applyProtocol(p *Protocol)
}

type EndpointOption interface {
	applyEndpoint(e *Endpoint)
}

type Option interface {
	ProtocolOption
	EndpointOption
}

type withBufferPool struct{ pool *Pool }

func (o withBufferPool) applyProtocol(p *Protocol)         { p.pool = o.pool }
func (o withBufferPool) applyEndpoint(e *Endpoint) { e.pool = o.pool }

func WithBufferPool(pool *Pool) Option { return withBufferPool{pool: pool} }

type withWriteBufferSize struct{ writeBufferSize uint16 }

func (o withWriteBufferSize) applyProtocol(p *Protocol)         { p.writeBufferSize = o.writeBufferSize }
func (o withWriteBufferSize) applyEndpoint(e *Endpoint) { e.writeBufferSize = o.writeBufferSize }

func WithWriteBufferSize(writeBufferSize uint16) Option {
	if 65536%uint32(writeBufferSize) != 0 {
		panic("write buffer size must be smaller than 65536 and a power of two")
	}
	return withWriteBufferSize{writeBufferSize: writeBufferSize}
}

type withReadBufferSize struct{ readBufferSize uint16 }

func (o withReadBufferSize) applyProtocol(p *Protocol)         { p.readBufferSize = o.readBufferSize }
func (o withReadBufferSize) applyEndpoint(e *Endpoint) { e.readBufferSize = o.readBufferSize }

func WithReadBufferSize(readBufferSize uint16) Option {
	if 65536%uint32(readBufferSize) != 0 {
		panic("read buffer size must be smaller than 65536 and a power of two")
	}
	return withReadBufferSize{readBufferSize: readBufferSize}
}

type withPacketHandler struct{ ph PacketHandler }

func (o withPacketHandler) applyProtocol(p *Protocol)         { p.ph = o.ph }
func (o withPacketHandler) applyEndpoint(e *Endpoint) { e.ph = o.ph }

func WithPacketHandler(ph PacketHandler) Option { return withPacketHandler{ph: ph} }

type withErrorHandler struct{ eh ErrorHandler }

func (o withErrorHandler) applyProtocol(p *Protocol)         { p.eh = o.eh }
func (o withErrorHandler) applyEndpoint(e *Endpoint) { e.eh = o.eh }

func WithErrorHandler(eh ErrorHandler) Option { return withErrorHandler{eh: eh} }

type withUpdatePeriod struct{ updatePeriod time.Duration }

func (o withUpdatePeriod) applyProtocol(p *Protocol)         { p.updatePeriod = o.updatePeriod }
func (o withUpdatePeriod) applyEndpoint(e *Endpoint) { e.updatePeriod = o.updatePeriod }

func WithUpdatePeriod(updatePeriod time.Duration) Option {
	if updatePeriod == 0 {
		panic("update period of zero is not supported yet")
	}
	return withUpdatePeriod{updatePeriod: updatePeriod}
}

type withResendTimeout struct{ resendTimeout time.Duration }

func (o withResendTimeout) applyProtocol(p *Protocol)         { p.resendTimeout = o.resendTimeout }
func (o withResendTimeout) applyEndpoint(e *Endpoint) { e.resendTimeout = o.resendTimeout }

func WithResendTimeout(resendTimeout time.Duration) Option {
	if resendTimeout == 0 {
		panic("ack timeout of zero is not supported yet")
	}
	return withResendTimeout{resendTimeout: resendTimeout}
}
