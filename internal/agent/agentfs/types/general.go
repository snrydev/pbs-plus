package types

import (
	"github.com/pbs-plus/pbs-plus/internal/arpc/arpcdata"
)

// HandleId is a type alias for uint64
type HandleId uint64

func (id *HandleId) Encode() ([]byte, error) {
	enc := arpcdata.NewEncoderWithSize(8)
	if err := enc.WriteUint64(uint64(*id)); err != nil {
		return nil, err
	}
	return enc.Bytes(), nil
}

func (id *HandleId) Decode(buf []byte) error {
	dec, err := arpcdata.NewDecoder(buf)
	if err != nil {
		return err
	}
	value, err := dec.ReadUint64()
	if err != nil {
		return err
	}
	*id = HandleId(value)

	arpcdata.ReleaseDecoder(dec)
	return nil
}

