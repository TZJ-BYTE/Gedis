package datastruct

import "testing"

func TestDataValue_V3RoundTrip_String(t *testing.T) {
	dv := &DataValue{
		Value:          &BytesString{Data: []byte("hello")},
		ExpireTime:     123,
		LastAccessedAt: 456,
	}
	b, err := dv.Serialize()
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	out, err := DeserializeDataValue(b)
	if err != nil {
		t.Fatalf("deserialize: %v", err)
	}
	v, ok := out.Value.(*BytesString)
	if !ok {
		t.Fatalf("type: %T", out.Value)
	}
	if string(v.Data) != "hello" {
		t.Fatalf("value: %q", string(v.Data))
	}
	if out.ExpireTime != 123 || out.LastAccessedAt != 456 {
		t.Fatalf("meta: %d %d", out.ExpireTime, out.LastAccessedAt)
	}
}

func TestDataValue_V3RoundTrip_List(t *testing.T) {
	dv := &DataValue{
		Value:          &List{Data: []string{"a", "", "c"}},
		ExpireTime:     1,
		LastAccessedAt: 2,
	}
	b, err := dv.Serialize()
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	out, err := DeserializeDataValue(b)
	if err != nil {
		t.Fatalf("deserialize: %v", err)
	}
	v, ok := out.Value.(*List)
	if !ok {
		t.Fatalf("type: %T", out.Value)
	}
	if len(v.Data) != 3 || v.Data[0] != "a" || v.Data[1] != "" || v.Data[2] != "c" {
		t.Fatalf("value: %#v", v.Data)
	}
}

func TestDataValue_V3RoundTrip_Hash(t *testing.T) {
	dv := &DataValue{
		Value:          &Hash{Data: map[string]string{"b": "2", "a": "1"}},
		ExpireTime:     1,
		LastAccessedAt: 2,
	}
	b, err := dv.Serialize()
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	out, err := DeserializeDataValue(b)
	if err != nil {
		t.Fatalf("deserialize: %v", err)
	}
	v, ok := out.Value.(*Hash)
	if !ok {
		t.Fatalf("type: %T", out.Value)
	}
	if v.Data["a"] != "1" || v.Data["b"] != "2" {
		t.Fatalf("value: %#v", v.Data)
	}
}

