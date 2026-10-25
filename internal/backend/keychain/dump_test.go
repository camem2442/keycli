package keychain_test

import (
	"reflect"
	"testing"

	"github.com/camem2442/keycli/internal/backend/keychain"
)

const realDump = `keychain: "/Users/x/Library/Keychains/login.keychain-db"
version: 512
class: 0x00000010 
attributes:
    0x00000007 <blob>="local.study.ai.openai"
    0x00000008 <blob>=<NULL>
    "acct"<blob>="openai_api_key"
    "cdat"<timedate>=0x32303235303430313136353730355A00  "20250401165705Z\000"
    "crtr"<uint32>=<NULL>
    "svce"<blob>="local.study.ai.openai"
    "type"<uint32>=<NULL>
class: 0x00000010 
attributes:
    0x00000007 <blob>="kc.secret.plain"
    "acct"<blob>=<NULL>
    "svce"<blob>="kc.secret.plain"
    "type"<uint32>=<NULL>
class: 0x00000010 
attributes:
    0x00000007 <blob>="kc.secret.weird name"
    "acct"<blob>="kc"
    "svce"<blob>="kc.secret.weird\040name"
    "type"<uint32>=<NULL>
`

func TestParseDumpEntriesRealFormat(t *testing.T) {
	got := keychain.ParseDumpEntries(realDump)
	want := []keychain.DumpEntry{
		{Service: "local.study.ai.openai", Account: "openai_api_key"},
		{Service: "kc.secret.plain"},
		{Service: "kc.secret.weird name", Account: "kc"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("entries:\n got %#v\nwant %#v", got, want)
	}
}

func TestParseDumpEntriesLegacyFake(t *testing.T) {
	got := keychain.ParseDumpEntries("keychain: \"/Fake\"\nservice: kc.secret.old\nservice: local.study.ai.groq\n")
	want := []keychain.DumpEntry{
		{Service: "kc.secret.old"},
		{Service: "local.study.ai.groq"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("entries:\n got %#v\nwant %#v", got, want)
	}
}

func TestListAICoreItemsRealFormat(t *testing.T) {
	got := keychain.ListAICoreItems(realDump, "local.study.ai")
	if !reflect.DeepEqual(got, []string{"openai"}) {
		t.Fatalf("items %#v", got)
	}
}
