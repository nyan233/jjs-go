package compiler

var (
	gDecoder = new(decoderImpl).init()
)

type decoderImpl struct {
	baseCache
}

func (d *decoderImpl) init() *decoderImpl {
	d.baseCache.init()
	return d
}

func (d *decoderImpl) Compile() (DecodeHandler, error) {
	return nil, nil
}
