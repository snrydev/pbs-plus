package controllers

// Code generated by github.com/tinylib/msgp DO NOT EDIT.

import (
	"github.com/tinylib/msgp/msgp"
)

// DecodeMsg implements msgp.Decodable
func (z *BackupReq) DecodeMsg(dc *msgp.Reader) (err error) {
	var field []byte
	_ = field
	var zb0001 uint32
	zb0001, err = dc.ReadMapHeader()
	if err != nil {
		err = msgp.WrapError(err)
		return
	}
	for zb0001 > 0 {
		zb0001--
		field, err = dc.ReadMapKeyPtr()
		if err != nil {
			err = msgp.WrapError(err)
			return
		}
		switch msgp.UnsafeString(field) {
		case "job_id":
			z.JobId, err = dc.ReadString()
			if err != nil {
				err = msgp.WrapError(err, "JobId")
				return
			}
		case "drive":
			z.Drive, err = dc.ReadString()
			if err != nil {
				err = msgp.WrapError(err, "Drive")
				return
			}
		default:
			err = dc.Skip()
			if err != nil {
				err = msgp.WrapError(err)
				return
			}
		}
	}
	return
}

// EncodeMsg implements msgp.Encodable
func (z BackupReq) EncodeMsg(en *msgp.Writer) (err error) {
	// map header, size 2
	// write "job_id"
	err = en.Append(0x82, 0xa6, 0x6a, 0x6f, 0x62, 0x5f, 0x69, 0x64)
	if err != nil {
		return
	}
	err = en.WriteString(z.JobId)
	if err != nil {
		err = msgp.WrapError(err, "JobId")
		return
	}
	// write "drive"
	err = en.Append(0xa5, 0x64, 0x72, 0x69, 0x76, 0x65)
	if err != nil {
		return
	}
	err = en.WriteString(z.Drive)
	if err != nil {
		err = msgp.WrapError(err, "Drive")
		return
	}
	return
}

// MarshalMsg implements msgp.Marshaler
func (z BackupReq) MarshalMsg(b []byte) (o []byte, err error) {
	o = msgp.Require(b, z.Msgsize())
	// map header, size 2
	// string "job_id"
	o = append(o, 0x82, 0xa6, 0x6a, 0x6f, 0x62, 0x5f, 0x69, 0x64)
	o = msgp.AppendString(o, z.JobId)
	// string "drive"
	o = append(o, 0xa5, 0x64, 0x72, 0x69, 0x76, 0x65)
	o = msgp.AppendString(o, z.Drive)
	return
}

// UnmarshalMsg implements msgp.Unmarshaler
func (z *BackupReq) UnmarshalMsg(bts []byte) (o []byte, err error) {
	var field []byte
	_ = field
	var zb0001 uint32
	zb0001, bts, err = msgp.ReadMapHeaderBytes(bts)
	if err != nil {
		err = msgp.WrapError(err)
		return
	}
	for zb0001 > 0 {
		zb0001--
		field, bts, err = msgp.ReadMapKeyZC(bts)
		if err != nil {
			err = msgp.WrapError(err)
			return
		}
		switch msgp.UnsafeString(field) {
		case "job_id":
			z.JobId, bts, err = msgp.ReadStringBytes(bts)
			if err != nil {
				err = msgp.WrapError(err, "JobId")
				return
			}
		case "drive":
			z.Drive, bts, err = msgp.ReadStringBytes(bts)
			if err != nil {
				err = msgp.WrapError(err, "Drive")
				return
			}
		default:
			bts, err = msgp.Skip(bts)
			if err != nil {
				err = msgp.WrapError(err)
				return
			}
		}
	}
	o = bts
	return
}

// Msgsize returns an upper bound estimate of the number of bytes occupied by the serialized message
func (z BackupReq) Msgsize() (s int) {
	s = 1 + 7 + msgp.StringPrefixSize + len(z.JobId) + 6 + msgp.StringPrefixSize + len(z.Drive)
	return
}
