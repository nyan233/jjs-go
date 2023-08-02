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
	Uid1 string
	Uid4 string
	UID5 string
}

func TestJsonMarshaller(t *testing.T) {
	td1 := &JsonT2{
		Uid1: "xiaoming233",
		Uid4: "zhouzhoujun666",
		UID5: "lixiaomei666",
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
		Uid1: gen(2048 + 1024),
		Uid4: gen(4096),
		UID5: gen(4096),
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
}

func gen(size int) string {
	b := make([]byte, 0, size)
	for i := 0; i < size; i++ {
		b = append(b, 'h')
	}
	return string(b)
}
