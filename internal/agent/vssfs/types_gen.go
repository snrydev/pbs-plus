package vssfs

// Code generated by github.com/tinylib/msgp DO NOT EDIT.

import (
	"github.com/tinylib/msgp/msgp"
)

// DecodeMsg implements msgp.Decodable
func (z *CloseReq) DecodeMsg(dc *msgp.Reader) (err error) {
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
		case "handleID":
			z.HandleID, err = dc.ReadInt()
			if err != nil {
				err = msgp.WrapError(err, "HandleID")
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
func (z CloseReq) EncodeMsg(en *msgp.Writer) (err error) {
	// map header, size 1
	// write "handleID"
	err = en.Append(0x81, 0xa8, 0x68, 0x61, 0x6e, 0x64, 0x6c, 0x65, 0x49, 0x44)
	if err != nil {
		return
	}
	err = en.WriteInt(z.HandleID)
	if err != nil {
		err = msgp.WrapError(err, "HandleID")
		return
	}
	return
}

// MarshalMsg implements msgp.Marshaler
func (z CloseReq) MarshalMsg(b []byte) (o []byte, err error) {
	o = msgp.Require(b, z.Msgsize())
	// map header, size 1
	// string "handleID"
	o = append(o, 0x81, 0xa8, 0x68, 0x61, 0x6e, 0x64, 0x6c, 0x65, 0x49, 0x44)
	o = msgp.AppendInt(o, z.HandleID)
	return
}

// UnmarshalMsg implements msgp.Unmarshaler
func (z *CloseReq) UnmarshalMsg(bts []byte) (o []byte, err error) {
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
		case "handleID":
			z.HandleID, bts, err = msgp.ReadIntBytes(bts)
			if err != nil {
				err = msgp.WrapError(err, "HandleID")
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
func (z CloseReq) Msgsize() (s int) {
	s = 1 + 9 + msgp.IntSize
	return
}

// DecodeMsg implements msgp.Decodable
func (z *DataResponse) DecodeMsg(dc *msgp.Reader) (err error) {
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
		case "data":
			z.Data, err = dc.ReadBytes(z.Data)
			if err != nil {
				err = msgp.WrapError(err, "Data")
				return
			}
		case "eof":
			z.EOF, err = dc.ReadBool()
			if err != nil {
				err = msgp.WrapError(err, "EOF")
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
func (z *DataResponse) EncodeMsg(en *msgp.Writer) (err error) {
	// map header, size 2
	// write "data"
	err = en.Append(0x82, 0xa4, 0x64, 0x61, 0x74, 0x61)
	if err != nil {
		return
	}
	err = en.WriteBytes(z.Data)
	if err != nil {
		err = msgp.WrapError(err, "Data")
		return
	}
	// write "eof"
	err = en.Append(0xa3, 0x65, 0x6f, 0x66)
	if err != nil {
		return
	}
	err = en.WriteBool(z.EOF)
	if err != nil {
		err = msgp.WrapError(err, "EOF")
		return
	}
	return
}

// MarshalMsg implements msgp.Marshaler
func (z *DataResponse) MarshalMsg(b []byte) (o []byte, err error) {
	o = msgp.Require(b, z.Msgsize())
	// map header, size 2
	// string "data"
	o = append(o, 0x82, 0xa4, 0x64, 0x61, 0x74, 0x61)
	o = msgp.AppendBytes(o, z.Data)
	// string "eof"
	o = append(o, 0xa3, 0x65, 0x6f, 0x66)
	o = msgp.AppendBool(o, z.EOF)
	return
}

// UnmarshalMsg implements msgp.Unmarshaler
func (z *DataResponse) UnmarshalMsg(bts []byte) (o []byte, err error) {
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
		case "data":
			z.Data, bts, err = msgp.ReadBytesBytes(bts, z.Data)
			if err != nil {
				err = msgp.WrapError(err, "Data")
				return
			}
		case "eof":
			z.EOF, bts, err = msgp.ReadBoolBytes(bts)
			if err != nil {
				err = msgp.WrapError(err, "EOF")
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
func (z *DataResponse) Msgsize() (s int) {
	s = 1 + 5 + msgp.BytesPrefixSize + len(z.Data) + 4 + msgp.BoolSize
	return
}

// DecodeMsg implements msgp.Decodable
func (z *FSStat) DecodeMsg(dc *msgp.Reader) (err error) {
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
		case "total_size":
			z.TotalSize, err = dc.ReadInt64()
			if err != nil {
				err = msgp.WrapError(err, "TotalSize")
				return
			}
		case "free_size":
			z.FreeSize, err = dc.ReadInt64()
			if err != nil {
				err = msgp.WrapError(err, "FreeSize")
				return
			}
		case "available_size":
			z.AvailableSize, err = dc.ReadInt64()
			if err != nil {
				err = msgp.WrapError(err, "AvailableSize")
				return
			}
		case "total_files":
			z.TotalFiles, err = dc.ReadInt()
			if err != nil {
				err = msgp.WrapError(err, "TotalFiles")
				return
			}
		case "free_files":
			z.FreeFiles, err = dc.ReadInt()
			if err != nil {
				err = msgp.WrapError(err, "FreeFiles")
				return
			}
		case "available_files":
			z.AvailableFiles, err = dc.ReadInt()
			if err != nil {
				err = msgp.WrapError(err, "AvailableFiles")
				return
			}
		case "cache_hint":
			z.CacheHint, err = dc.ReadDuration()
			if err != nil {
				err = msgp.WrapError(err, "CacheHint")
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
func (z *FSStat) EncodeMsg(en *msgp.Writer) (err error) {
	// map header, size 7
	// write "total_size"
	err = en.Append(0x87, 0xaa, 0x74, 0x6f, 0x74, 0x61, 0x6c, 0x5f, 0x73, 0x69, 0x7a, 0x65)
	if err != nil {
		return
	}
	err = en.WriteInt64(z.TotalSize)
	if err != nil {
		err = msgp.WrapError(err, "TotalSize")
		return
	}
	// write "free_size"
	err = en.Append(0xa9, 0x66, 0x72, 0x65, 0x65, 0x5f, 0x73, 0x69, 0x7a, 0x65)
	if err != nil {
		return
	}
	err = en.WriteInt64(z.FreeSize)
	if err != nil {
		err = msgp.WrapError(err, "FreeSize")
		return
	}
	// write "available_size"
	err = en.Append(0xae, 0x61, 0x76, 0x61, 0x69, 0x6c, 0x61, 0x62, 0x6c, 0x65, 0x5f, 0x73, 0x69, 0x7a, 0x65)
	if err != nil {
		return
	}
	err = en.WriteInt64(z.AvailableSize)
	if err != nil {
		err = msgp.WrapError(err, "AvailableSize")
		return
	}
	// write "total_files"
	err = en.Append(0xab, 0x74, 0x6f, 0x74, 0x61, 0x6c, 0x5f, 0x66, 0x69, 0x6c, 0x65, 0x73)
	if err != nil {
		return
	}
	err = en.WriteInt(z.TotalFiles)
	if err != nil {
		err = msgp.WrapError(err, "TotalFiles")
		return
	}
	// write "free_files"
	err = en.Append(0xaa, 0x66, 0x72, 0x65, 0x65, 0x5f, 0x66, 0x69, 0x6c, 0x65, 0x73)
	if err != nil {
		return
	}
	err = en.WriteInt(z.FreeFiles)
	if err != nil {
		err = msgp.WrapError(err, "FreeFiles")
		return
	}
	// write "available_files"
	err = en.Append(0xaf, 0x61, 0x76, 0x61, 0x69, 0x6c, 0x61, 0x62, 0x6c, 0x65, 0x5f, 0x66, 0x69, 0x6c, 0x65, 0x73)
	if err != nil {
		return
	}
	err = en.WriteInt(z.AvailableFiles)
	if err != nil {
		err = msgp.WrapError(err, "AvailableFiles")
		return
	}
	// write "cache_hint"
	err = en.Append(0xaa, 0x63, 0x61, 0x63, 0x68, 0x65, 0x5f, 0x68, 0x69, 0x6e, 0x74)
	if err != nil {
		return
	}
	err = en.WriteDuration(z.CacheHint)
	if err != nil {
		err = msgp.WrapError(err, "CacheHint")
		return
	}
	return
}

// MarshalMsg implements msgp.Marshaler
func (z *FSStat) MarshalMsg(b []byte) (o []byte, err error) {
	o = msgp.Require(b, z.Msgsize())
	// map header, size 7
	// string "total_size"
	o = append(o, 0x87, 0xaa, 0x74, 0x6f, 0x74, 0x61, 0x6c, 0x5f, 0x73, 0x69, 0x7a, 0x65)
	o = msgp.AppendInt64(o, z.TotalSize)
	// string "free_size"
	o = append(o, 0xa9, 0x66, 0x72, 0x65, 0x65, 0x5f, 0x73, 0x69, 0x7a, 0x65)
	o = msgp.AppendInt64(o, z.FreeSize)
	// string "available_size"
	o = append(o, 0xae, 0x61, 0x76, 0x61, 0x69, 0x6c, 0x61, 0x62, 0x6c, 0x65, 0x5f, 0x73, 0x69, 0x7a, 0x65)
	o = msgp.AppendInt64(o, z.AvailableSize)
	// string "total_files"
	o = append(o, 0xab, 0x74, 0x6f, 0x74, 0x61, 0x6c, 0x5f, 0x66, 0x69, 0x6c, 0x65, 0x73)
	o = msgp.AppendInt(o, z.TotalFiles)
	// string "free_files"
	o = append(o, 0xaa, 0x66, 0x72, 0x65, 0x65, 0x5f, 0x66, 0x69, 0x6c, 0x65, 0x73)
	o = msgp.AppendInt(o, z.FreeFiles)
	// string "available_files"
	o = append(o, 0xaf, 0x61, 0x76, 0x61, 0x69, 0x6c, 0x61, 0x62, 0x6c, 0x65, 0x5f, 0x66, 0x69, 0x6c, 0x65, 0x73)
	o = msgp.AppendInt(o, z.AvailableFiles)
	// string "cache_hint"
	o = append(o, 0xaa, 0x63, 0x61, 0x63, 0x68, 0x65, 0x5f, 0x68, 0x69, 0x6e, 0x74)
	o = msgp.AppendDuration(o, z.CacheHint)
	return
}

// UnmarshalMsg implements msgp.Unmarshaler
func (z *FSStat) UnmarshalMsg(bts []byte) (o []byte, err error) {
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
		case "total_size":
			z.TotalSize, bts, err = msgp.ReadInt64Bytes(bts)
			if err != nil {
				err = msgp.WrapError(err, "TotalSize")
				return
			}
		case "free_size":
			z.FreeSize, bts, err = msgp.ReadInt64Bytes(bts)
			if err != nil {
				err = msgp.WrapError(err, "FreeSize")
				return
			}
		case "available_size":
			z.AvailableSize, bts, err = msgp.ReadInt64Bytes(bts)
			if err != nil {
				err = msgp.WrapError(err, "AvailableSize")
				return
			}
		case "total_files":
			z.TotalFiles, bts, err = msgp.ReadIntBytes(bts)
			if err != nil {
				err = msgp.WrapError(err, "TotalFiles")
				return
			}
		case "free_files":
			z.FreeFiles, bts, err = msgp.ReadIntBytes(bts)
			if err != nil {
				err = msgp.WrapError(err, "FreeFiles")
				return
			}
		case "available_files":
			z.AvailableFiles, bts, err = msgp.ReadIntBytes(bts)
			if err != nil {
				err = msgp.WrapError(err, "AvailableFiles")
				return
			}
		case "cache_hint":
			z.CacheHint, bts, err = msgp.ReadDurationBytes(bts)
			if err != nil {
				err = msgp.WrapError(err, "CacheHint")
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
func (z *FSStat) Msgsize() (s int) {
	s = 1 + 11 + msgp.Int64Size + 10 + msgp.Int64Size + 15 + msgp.Int64Size + 12 + msgp.IntSize + 11 + msgp.IntSize + 16 + msgp.IntSize + 11 + msgp.DurationSize
	return
}

// DecodeMsg implements msgp.Decodable
func (z *FileHandleId) DecodeMsg(dc *msgp.Reader) (err error) {
	{
		var zb0001 int
		zb0001, err = dc.ReadInt()
		if err != nil {
			err = msgp.WrapError(err)
			return
		}
		(*z) = FileHandleId(zb0001)
	}
	return
}

// EncodeMsg implements msgp.Encodable
func (z FileHandleId) EncodeMsg(en *msgp.Writer) (err error) {
	err = en.WriteInt(int(z))
	if err != nil {
		err = msgp.WrapError(err)
		return
	}
	return
}

// MarshalMsg implements msgp.Marshaler
func (z FileHandleId) MarshalMsg(b []byte) (o []byte, err error) {
	o = msgp.Require(b, z.Msgsize())
	o = msgp.AppendInt(o, int(z))
	return
}

// UnmarshalMsg implements msgp.Unmarshaler
func (z *FileHandleId) UnmarshalMsg(bts []byte) (o []byte, err error) {
	{
		var zb0001 int
		zb0001, bts, err = msgp.ReadIntBytes(bts)
		if err != nil {
			err = msgp.WrapError(err)
			return
		}
		(*z) = FileHandleId(zb0001)
	}
	o = bts
	return
}

// Msgsize returns an upper bound estimate of the number of bytes occupied by the serialized message
func (z FileHandleId) Msgsize() (s int) {
	s = msgp.IntSize
	return
}

// DecodeMsg implements msgp.Decodable
func (z *FstatReq) DecodeMsg(dc *msgp.Reader) (err error) {
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
		case "handleID":
			z.HandleID, err = dc.ReadInt()
			if err != nil {
				err = msgp.WrapError(err, "HandleID")
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
func (z FstatReq) EncodeMsg(en *msgp.Writer) (err error) {
	// map header, size 1
	// write "handleID"
	err = en.Append(0x81, 0xa8, 0x68, 0x61, 0x6e, 0x64, 0x6c, 0x65, 0x49, 0x44)
	if err != nil {
		return
	}
	err = en.WriteInt(z.HandleID)
	if err != nil {
		err = msgp.WrapError(err, "HandleID")
		return
	}
	return
}

// MarshalMsg implements msgp.Marshaler
func (z FstatReq) MarshalMsg(b []byte) (o []byte, err error) {
	o = msgp.Require(b, z.Msgsize())
	// map header, size 1
	// string "handleID"
	o = append(o, 0x81, 0xa8, 0x68, 0x61, 0x6e, 0x64, 0x6c, 0x65, 0x49, 0x44)
	o = msgp.AppendInt(o, z.HandleID)
	return
}

// UnmarshalMsg implements msgp.Unmarshaler
func (z *FstatReq) UnmarshalMsg(bts []byte) (o []byte, err error) {
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
		case "handleID":
			z.HandleID, bts, err = msgp.ReadIntBytes(bts)
			if err != nil {
				err = msgp.WrapError(err, "HandleID")
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
func (z FstatReq) Msgsize() (s int) {
	s = 1 + 9 + msgp.IntSize
	return
}

// DecodeMsg implements msgp.Decodable
func (z *OpenFileReq) DecodeMsg(dc *msgp.Reader) (err error) {
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
		case "path":
			z.Path, err = dc.ReadString()
			if err != nil {
				err = msgp.WrapError(err, "Path")
				return
			}
		case "flag":
			z.Flag, err = dc.ReadInt()
			if err != nil {
				err = msgp.WrapError(err, "Flag")
				return
			}
		case "perm":
			z.Perm, err = dc.ReadInt()
			if err != nil {
				err = msgp.WrapError(err, "Perm")
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
func (z OpenFileReq) EncodeMsg(en *msgp.Writer) (err error) {
	// map header, size 3
	// write "path"
	err = en.Append(0x83, 0xa4, 0x70, 0x61, 0x74, 0x68)
	if err != nil {
		return
	}
	err = en.WriteString(z.Path)
	if err != nil {
		err = msgp.WrapError(err, "Path")
		return
	}
	// write "flag"
	err = en.Append(0xa4, 0x66, 0x6c, 0x61, 0x67)
	if err != nil {
		return
	}
	err = en.WriteInt(z.Flag)
	if err != nil {
		err = msgp.WrapError(err, "Flag")
		return
	}
	// write "perm"
	err = en.Append(0xa4, 0x70, 0x65, 0x72, 0x6d)
	if err != nil {
		return
	}
	err = en.WriteInt(z.Perm)
	if err != nil {
		err = msgp.WrapError(err, "Perm")
		return
	}
	return
}

// MarshalMsg implements msgp.Marshaler
func (z OpenFileReq) MarshalMsg(b []byte) (o []byte, err error) {
	o = msgp.Require(b, z.Msgsize())
	// map header, size 3
	// string "path"
	o = append(o, 0x83, 0xa4, 0x70, 0x61, 0x74, 0x68)
	o = msgp.AppendString(o, z.Path)
	// string "flag"
	o = append(o, 0xa4, 0x66, 0x6c, 0x61, 0x67)
	o = msgp.AppendInt(o, z.Flag)
	// string "perm"
	o = append(o, 0xa4, 0x70, 0x65, 0x72, 0x6d)
	o = msgp.AppendInt(o, z.Perm)
	return
}

// UnmarshalMsg implements msgp.Unmarshaler
func (z *OpenFileReq) UnmarshalMsg(bts []byte) (o []byte, err error) {
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
		case "path":
			z.Path, bts, err = msgp.ReadStringBytes(bts)
			if err != nil {
				err = msgp.WrapError(err, "Path")
				return
			}
		case "flag":
			z.Flag, bts, err = msgp.ReadIntBytes(bts)
			if err != nil {
				err = msgp.WrapError(err, "Flag")
				return
			}
		case "perm":
			z.Perm, bts, err = msgp.ReadIntBytes(bts)
			if err != nil {
				err = msgp.WrapError(err, "Perm")
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
func (z OpenFileReq) Msgsize() (s int) {
	s = 1 + 5 + msgp.StringPrefixSize + len(z.Path) + 5 + msgp.IntSize + 5 + msgp.IntSize
	return
}

// DecodeMsg implements msgp.Decodable
func (z *ReadAtReq) DecodeMsg(dc *msgp.Reader) (err error) {
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
		case "handleID":
			z.HandleID, err = dc.ReadInt()
			if err != nil {
				err = msgp.WrapError(err, "HandleID")
				return
			}
		case "offset":
			z.Offset, err = dc.ReadInt64()
			if err != nil {
				err = msgp.WrapError(err, "Offset")
				return
			}
		case "length":
			z.Length, err = dc.ReadInt()
			if err != nil {
				err = msgp.WrapError(err, "Length")
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
func (z ReadAtReq) EncodeMsg(en *msgp.Writer) (err error) {
	// map header, size 3
	// write "handleID"
	err = en.Append(0x83, 0xa8, 0x68, 0x61, 0x6e, 0x64, 0x6c, 0x65, 0x49, 0x44)
	if err != nil {
		return
	}
	err = en.WriteInt(z.HandleID)
	if err != nil {
		err = msgp.WrapError(err, "HandleID")
		return
	}
	// write "offset"
	err = en.Append(0xa6, 0x6f, 0x66, 0x66, 0x73, 0x65, 0x74)
	if err != nil {
		return
	}
	err = en.WriteInt64(z.Offset)
	if err != nil {
		err = msgp.WrapError(err, "Offset")
		return
	}
	// write "length"
	err = en.Append(0xa6, 0x6c, 0x65, 0x6e, 0x67, 0x74, 0x68)
	if err != nil {
		return
	}
	err = en.WriteInt(z.Length)
	if err != nil {
		err = msgp.WrapError(err, "Length")
		return
	}
	return
}

// MarshalMsg implements msgp.Marshaler
func (z ReadAtReq) MarshalMsg(b []byte) (o []byte, err error) {
	o = msgp.Require(b, z.Msgsize())
	// map header, size 3
	// string "handleID"
	o = append(o, 0x83, 0xa8, 0x68, 0x61, 0x6e, 0x64, 0x6c, 0x65, 0x49, 0x44)
	o = msgp.AppendInt(o, z.HandleID)
	// string "offset"
	o = append(o, 0xa6, 0x6f, 0x66, 0x66, 0x73, 0x65, 0x74)
	o = msgp.AppendInt64(o, z.Offset)
	// string "length"
	o = append(o, 0xa6, 0x6c, 0x65, 0x6e, 0x67, 0x74, 0x68)
	o = msgp.AppendInt(o, z.Length)
	return
}

// UnmarshalMsg implements msgp.Unmarshaler
func (z *ReadAtReq) UnmarshalMsg(bts []byte) (o []byte, err error) {
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
		case "handleID":
			z.HandleID, bts, err = msgp.ReadIntBytes(bts)
			if err != nil {
				err = msgp.WrapError(err, "HandleID")
				return
			}
		case "offset":
			z.Offset, bts, err = msgp.ReadInt64Bytes(bts)
			if err != nil {
				err = msgp.WrapError(err, "Offset")
				return
			}
		case "length":
			z.Length, bts, err = msgp.ReadIntBytes(bts)
			if err != nil {
				err = msgp.WrapError(err, "Length")
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
func (z ReadAtReq) Msgsize() (s int) {
	s = 1 + 9 + msgp.IntSize + 7 + msgp.Int64Size + 7 + msgp.IntSize
	return
}

// DecodeMsg implements msgp.Decodable
func (z *ReadDirEntries) DecodeMsg(dc *msgp.Reader) (err error) {
	var zb0002 uint32
	zb0002, err = dc.ReadArrayHeader()
	if err != nil {
		err = msgp.WrapError(err)
		return
	}
	if cap((*z)) >= int(zb0002) {
		(*z) = (*z)[:zb0002]
	} else {
		(*z) = make(ReadDirEntries, zb0002)
	}
	for zb0001 := range *z {
		if dc.IsNil() {
			err = dc.ReadNil()
			if err != nil {
				err = msgp.WrapError(err, zb0001)
				return
			}
			(*z)[zb0001] = nil
		} else {
			if (*z)[zb0001] == nil {
				(*z)[zb0001] = new(VSSFileInfo)
			}
			err = (*z)[zb0001].DecodeMsg(dc)
			if err != nil {
				err = msgp.WrapError(err, zb0001)
				return
			}
		}
	}
	return
}

// EncodeMsg implements msgp.Encodable
func (z ReadDirEntries) EncodeMsg(en *msgp.Writer) (err error) {
	err = en.WriteArrayHeader(uint32(len(z)))
	if err != nil {
		err = msgp.WrapError(err)
		return
	}
	for zb0003 := range z {
		if z[zb0003] == nil {
			err = en.WriteNil()
			if err != nil {
				return
			}
		} else {
			err = z[zb0003].EncodeMsg(en)
			if err != nil {
				err = msgp.WrapError(err, zb0003)
				return
			}
		}
	}
	return
}

// MarshalMsg implements msgp.Marshaler
func (z ReadDirEntries) MarshalMsg(b []byte) (o []byte, err error) {
	o = msgp.Require(b, z.Msgsize())
	o = msgp.AppendArrayHeader(o, uint32(len(z)))
	for zb0003 := range z {
		if z[zb0003] == nil {
			o = msgp.AppendNil(o)
		} else {
			o, err = z[zb0003].MarshalMsg(o)
			if err != nil {
				err = msgp.WrapError(err, zb0003)
				return
			}
		}
	}
	return
}

// UnmarshalMsg implements msgp.Unmarshaler
func (z *ReadDirEntries) UnmarshalMsg(bts []byte) (o []byte, err error) {
	var zb0002 uint32
	zb0002, bts, err = msgp.ReadArrayHeaderBytes(bts)
	if err != nil {
		err = msgp.WrapError(err)
		return
	}
	if cap((*z)) >= int(zb0002) {
		(*z) = (*z)[:zb0002]
	} else {
		(*z) = make(ReadDirEntries, zb0002)
	}
	for zb0001 := range *z {
		if msgp.IsNil(bts) {
			bts, err = msgp.ReadNilBytes(bts)
			if err != nil {
				return
			}
			(*z)[zb0001] = nil
		} else {
			if (*z)[zb0001] == nil {
				(*z)[zb0001] = new(VSSFileInfo)
			}
			bts, err = (*z)[zb0001].UnmarshalMsg(bts)
			if err != nil {
				err = msgp.WrapError(err, zb0001)
				return
			}
		}
	}
	o = bts
	return
}

// Msgsize returns an upper bound estimate of the number of bytes occupied by the serialized message
func (z ReadDirEntries) Msgsize() (s int) {
	s = msgp.ArrayHeaderSize
	for zb0003 := range z {
		if z[zb0003] == nil {
			s += msgp.NilSize
		} else {
			s += z[zb0003].Msgsize()
		}
	}
	return
}

// DecodeMsg implements msgp.Decodable
func (z *ReadDirReq) DecodeMsg(dc *msgp.Reader) (err error) {
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
		case "path":
			z.Path, err = dc.ReadString()
			if err != nil {
				err = msgp.WrapError(err, "Path")
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
func (z ReadDirReq) EncodeMsg(en *msgp.Writer) (err error) {
	// map header, size 1
	// write "path"
	err = en.Append(0x81, 0xa4, 0x70, 0x61, 0x74, 0x68)
	if err != nil {
		return
	}
	err = en.WriteString(z.Path)
	if err != nil {
		err = msgp.WrapError(err, "Path")
		return
	}
	return
}

// MarshalMsg implements msgp.Marshaler
func (z ReadDirReq) MarshalMsg(b []byte) (o []byte, err error) {
	o = msgp.Require(b, z.Msgsize())
	// map header, size 1
	// string "path"
	o = append(o, 0x81, 0xa4, 0x70, 0x61, 0x74, 0x68)
	o = msgp.AppendString(o, z.Path)
	return
}

// UnmarshalMsg implements msgp.Unmarshaler
func (z *ReadDirReq) UnmarshalMsg(bts []byte) (o []byte, err error) {
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
		case "path":
			z.Path, bts, err = msgp.ReadStringBytes(bts)
			if err != nil {
				err = msgp.WrapError(err, "Path")
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
func (z ReadDirReq) Msgsize() (s int) {
	s = 1 + 5 + msgp.StringPrefixSize + len(z.Path)
	return
}

// DecodeMsg implements msgp.Decodable
func (z *ReadReq) DecodeMsg(dc *msgp.Reader) (err error) {
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
		case "handleID":
			z.HandleID, err = dc.ReadInt()
			if err != nil {
				err = msgp.WrapError(err, "HandleID")
				return
			}
		case "length":
			z.Length, err = dc.ReadInt()
			if err != nil {
				err = msgp.WrapError(err, "Length")
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
func (z ReadReq) EncodeMsg(en *msgp.Writer) (err error) {
	// map header, size 2
	// write "handleID"
	err = en.Append(0x82, 0xa8, 0x68, 0x61, 0x6e, 0x64, 0x6c, 0x65, 0x49, 0x44)
	if err != nil {
		return
	}
	err = en.WriteInt(z.HandleID)
	if err != nil {
		err = msgp.WrapError(err, "HandleID")
		return
	}
	// write "length"
	err = en.Append(0xa6, 0x6c, 0x65, 0x6e, 0x67, 0x74, 0x68)
	if err != nil {
		return
	}
	err = en.WriteInt(z.Length)
	if err != nil {
		err = msgp.WrapError(err, "Length")
		return
	}
	return
}

// MarshalMsg implements msgp.Marshaler
func (z ReadReq) MarshalMsg(b []byte) (o []byte, err error) {
	o = msgp.Require(b, z.Msgsize())
	// map header, size 2
	// string "handleID"
	o = append(o, 0x82, 0xa8, 0x68, 0x61, 0x6e, 0x64, 0x6c, 0x65, 0x49, 0x44)
	o = msgp.AppendInt(o, z.HandleID)
	// string "length"
	o = append(o, 0xa6, 0x6c, 0x65, 0x6e, 0x67, 0x74, 0x68)
	o = msgp.AppendInt(o, z.Length)
	return
}

// UnmarshalMsg implements msgp.Unmarshaler
func (z *ReadReq) UnmarshalMsg(bts []byte) (o []byte, err error) {
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
		case "handleID":
			z.HandleID, bts, err = msgp.ReadIntBytes(bts)
			if err != nil {
				err = msgp.WrapError(err, "HandleID")
				return
			}
		case "length":
			z.Length, bts, err = msgp.ReadIntBytes(bts)
			if err != nil {
				err = msgp.WrapError(err, "Length")
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
func (z ReadReq) Msgsize() (s int) {
	s = 1 + 9 + msgp.IntSize + 7 + msgp.IntSize
	return
}

// DecodeMsg implements msgp.Decodable
func (z *StatReq) DecodeMsg(dc *msgp.Reader) (err error) {
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
		case "path":
			z.Path, err = dc.ReadString()
			if err != nil {
				err = msgp.WrapError(err, "Path")
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
func (z StatReq) EncodeMsg(en *msgp.Writer) (err error) {
	// map header, size 1
	// write "path"
	err = en.Append(0x81, 0xa4, 0x70, 0x61, 0x74, 0x68)
	if err != nil {
		return
	}
	err = en.WriteString(z.Path)
	if err != nil {
		err = msgp.WrapError(err, "Path")
		return
	}
	return
}

// MarshalMsg implements msgp.Marshaler
func (z StatReq) MarshalMsg(b []byte) (o []byte, err error) {
	o = msgp.Require(b, z.Msgsize())
	// map header, size 1
	// string "path"
	o = append(o, 0x81, 0xa4, 0x70, 0x61, 0x74, 0x68)
	o = msgp.AppendString(o, z.Path)
	return
}

// UnmarshalMsg implements msgp.Unmarshaler
func (z *StatReq) UnmarshalMsg(bts []byte) (o []byte, err error) {
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
		case "path":
			z.Path, bts, err = msgp.ReadStringBytes(bts)
			if err != nil {
				err = msgp.WrapError(err, "Path")
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
func (z StatReq) Msgsize() (s int) {
	s = 1 + 5 + msgp.StringPrefixSize + len(z.Path)
	return
}

// DecodeMsg implements msgp.Decodable
func (z *VSSFileInfo) DecodeMsg(dc *msgp.Reader) (err error) {
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
		case "name":
			z.Name, err = dc.ReadString()
			if err != nil {
				err = msgp.WrapError(err, "Name")
				return
			}
		case "size":
			z.Size, err = dc.ReadInt64()
			if err != nil {
				err = msgp.WrapError(err, "Size")
				return
			}
		case "mode":
			z.Mode, err = dc.ReadUint32()
			if err != nil {
				err = msgp.WrapError(err, "Mode")
				return
			}
		case "modTime":
			z.ModTime, err = dc.ReadTime()
			if err != nil {
				err = msgp.WrapError(err, "ModTime")
				return
			}
		case "isDir":
			z.IsDir, err = dc.ReadBool()
			if err != nil {
				err = msgp.WrapError(err, "IsDir")
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
func (z *VSSFileInfo) EncodeMsg(en *msgp.Writer) (err error) {
	// map header, size 5
	// write "name"
	err = en.Append(0x85, 0xa4, 0x6e, 0x61, 0x6d, 0x65)
	if err != nil {
		return
	}
	err = en.WriteString(z.Name)
	if err != nil {
		err = msgp.WrapError(err, "Name")
		return
	}
	// write "size"
	err = en.Append(0xa4, 0x73, 0x69, 0x7a, 0x65)
	if err != nil {
		return
	}
	err = en.WriteInt64(z.Size)
	if err != nil {
		err = msgp.WrapError(err, "Size")
		return
	}
	// write "mode"
	err = en.Append(0xa4, 0x6d, 0x6f, 0x64, 0x65)
	if err != nil {
		return
	}
	err = en.WriteUint32(z.Mode)
	if err != nil {
		err = msgp.WrapError(err, "Mode")
		return
	}
	// write "modTime"
	err = en.Append(0xa7, 0x6d, 0x6f, 0x64, 0x54, 0x69, 0x6d, 0x65)
	if err != nil {
		return
	}
	err = en.WriteTime(z.ModTime)
	if err != nil {
		err = msgp.WrapError(err, "ModTime")
		return
	}
	// write "isDir"
	err = en.Append(0xa5, 0x69, 0x73, 0x44, 0x69, 0x72)
	if err != nil {
		return
	}
	err = en.WriteBool(z.IsDir)
	if err != nil {
		err = msgp.WrapError(err, "IsDir")
		return
	}
	return
}

// MarshalMsg implements msgp.Marshaler
func (z *VSSFileInfo) MarshalMsg(b []byte) (o []byte, err error) {
	o = msgp.Require(b, z.Msgsize())
	// map header, size 5
	// string "name"
	o = append(o, 0x85, 0xa4, 0x6e, 0x61, 0x6d, 0x65)
	o = msgp.AppendString(o, z.Name)
	// string "size"
	o = append(o, 0xa4, 0x73, 0x69, 0x7a, 0x65)
	o = msgp.AppendInt64(o, z.Size)
	// string "mode"
	o = append(o, 0xa4, 0x6d, 0x6f, 0x64, 0x65)
	o = msgp.AppendUint32(o, z.Mode)
	// string "modTime"
	o = append(o, 0xa7, 0x6d, 0x6f, 0x64, 0x54, 0x69, 0x6d, 0x65)
	o = msgp.AppendTime(o, z.ModTime)
	// string "isDir"
	o = append(o, 0xa5, 0x69, 0x73, 0x44, 0x69, 0x72)
	o = msgp.AppendBool(o, z.IsDir)
	return
}

// UnmarshalMsg implements msgp.Unmarshaler
func (z *VSSFileInfo) UnmarshalMsg(bts []byte) (o []byte, err error) {
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
		case "name":
			z.Name, bts, err = msgp.ReadStringBytes(bts)
			if err != nil {
				err = msgp.WrapError(err, "Name")
				return
			}
		case "size":
			z.Size, bts, err = msgp.ReadInt64Bytes(bts)
			if err != nil {
				err = msgp.WrapError(err, "Size")
				return
			}
		case "mode":
			z.Mode, bts, err = msgp.ReadUint32Bytes(bts)
			if err != nil {
				err = msgp.WrapError(err, "Mode")
				return
			}
		case "modTime":
			z.ModTime, bts, err = msgp.ReadTimeBytes(bts)
			if err != nil {
				err = msgp.WrapError(err, "ModTime")
				return
			}
		case "isDir":
			z.IsDir, bts, err = msgp.ReadBoolBytes(bts)
			if err != nil {
				err = msgp.WrapError(err, "IsDir")
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
func (z *VSSFileInfo) Msgsize() (s int) {
	s = 1 + 5 + msgp.StringPrefixSize + len(z.Name) + 5 + msgp.Int64Size + 5 + msgp.Uint32Size + 8 + msgp.TimeSize + 6 + msgp.BoolSize
	return
}
