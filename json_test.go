package jjs_go

import (
	"encoding/json"
	"github.com/bytedance/sonic"
	"runtime/debug"
	"testing"
)

type JsonT1 struct {
	UserName  string
	UserName2 string
}

type JsonT2 struct {
	Uid1                                                  string
	Uid4                                                  string
	UID5                                                  string
	MyUidUIdUUUUUIIIIIIDDDDDD222323827873HHHH5555634ggfwe string
}

func TestJsonMarshaller(t *testing.T) {
	td1 := &JsonT2{
		Uid1: "xiaoming233",
		Uid4: "zhouzhoujun666",
		UID5: "lixiaomei666",
		MyUidUIdUUUUUIIIIIIDDDDDD222323827873HHHH5555634ggfwe: "hehe",
	}
	b, err := Marshal(td1)
	if err != nil {
		t.Fatal(err)
	}
	b, _ = Marshal(td1)
	t.Log(string(b))
	td2 := new(JsonT2)
	err = json.Unmarshal(b, td2)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(td2)
}

func BenchmarkMarshal(b *testing.B) {
	debug.SetGCPercent(-1)
	td1 := &JsonT2{
		Uid1: gen(64),
		Uid4: gen(64),
		UID5: gen(64),
		MyUidUIdUUUUUIIIIIIDDDDDD222323827873HHHH5555634ggfwe: gen(64),
	}
	b.Run("Stdlib", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			b, _ := json.Marshal(td1)
			_ = b
		}
	})
	b.Run("JJS", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			b, _ := Marshal(td1)
			_ = b
		}
	})
	b.Run("Sonic", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			b, _ := sonic.ConfigStd.Marshal(td1)
			_ = b
		}
	})
	b.Run("Static", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			bytes := make([]byte, 0, 256)
			staticMarshall(td1, &bytes)
		}
	})
}

func gen(size int) string {
	b := make([]byte, 0, size)
	for i := 0; i < size; i++ {
		b = append(b, 'h')
	}
	return string(b)
}

func staticMarshall(j *JsonT2, bytes *[]byte) {
	*bytes = append(*bytes, "{\"Uid1\" : "...)
	*bytes = append(*bytes, j.Uid1...)
	*bytes = append(*bytes, ',')
	*bytes = append(*bytes, "{\"Uid4\" : "...)
	*bytes = append(*bytes, j.Uid4...)
	*bytes = append(*bytes, ',')
	*bytes = append(*bytes, "{\"UID5\" : "...)
	*bytes = append(*bytes, j.UID5...)
	*bytes = append(*bytes, '}')
	*bytes = append(*bytes, ',')
	*bytes = append(*bytes, "{\"MyUidUIdUUUUUIIIIIIDDDDDD222323827873HHHH5555634ggfwe\" : "...)
	*bytes = append(*bytes, j.UID5...)
	*bytes = append(*bytes, '}')
}
