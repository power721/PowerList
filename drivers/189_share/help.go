package _189_share

import (
	"bytes"
	"encoding/xml"
	"strings"
	"time"
)

type Time time.Time

func (t *Time) UnmarshalJSON(b []byte) error { return t.Unmarshal(b) }
func (t *Time) UnmarshalXML(e *xml.Decoder, ee xml.StartElement) error {
	b, err := e.Token()
	if err != nil {
		return err
	}
	if b, ok := b.(xml.CharData); ok {
		if err = t.Unmarshal(b); err != nil {
			return err
		}
	}
	return e.Skip()
}
func (t *Time) Unmarshal(b []byte) error {
	bs := strings.Trim(string(b), "\"")
	var v time.Time
	var err error
	for _, f := range []string{"2006-01-02 15:04:05 -07", "Jan 2, 2006 15:04:05 PM -07"} {
		v, err = time.ParseInLocation(f, bs+" +08", time.Local)
		if err == nil {
			break
		}
	}
	*t = Time(v)
	return err
}

type String string

func (t *String) UnmarshalJSON(b []byte) error { return t.Unmarshal(b) }
func (t *String) UnmarshalXML(e *xml.Decoder, ee xml.StartElement) error {
	b, err := e.Token()
	if err != nil {
		return err
	}
	if b, ok := b.(xml.CharData); ok {
		if err = t.Unmarshal(b); err != nil {
			return err
		}
	}
	return e.Skip()
}
func (s *String) Unmarshal(b []byte) error {
	*s = String(bytes.Trim(b, "\""))
	return nil
}
