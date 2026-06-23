package lansync

import "testing"

func TestBeaconEncodeDecodeRoundTrip(t *testing.T) {
	b := Beacon{Hostname: "laptop", Tool: "codex", Port: 7777, Fingerprint: "abcd"}
	data, err := encodeBeacon(b)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := decodeBeacon(data)
	if !ok {
		t.Fatal("decode rejected a valid beacon")
	}
	if got.Hostname != "laptop" || got.Tool != "codex" || got.Port != 7777 || got.Fingerprint != "abcd" {
		t.Fatalf("round trip mismatch: %+v", got)
	}
	if got.Magic != beaconMagic {
		t.Fatalf("magic = %q", got.Magic)
	}
}

func TestDecodeBeaconRejectsJunk(t *testing.T) {
	cases := [][]byte{
		nil,
		[]byte("not json"),
		[]byte(`{"hostname":"x"}`),               // missing magic + port
		[]byte(`{"cct":"sync-v1","port":0}`),     // bad port
		[]byte(`{"cct":"wrong","port":10}`),      // wrong magic
		[]byte(`{"cct":"sync-v1","port":70000}`), // port out of range
	}
	for i, c := range cases {
		if _, ok := decodeBeacon(c); ok {
			t.Errorf("case %d: expected reject", i)
		}
	}
}

func TestDedupeBeacons(t *testing.T) {
	in := []Beacon{
		{Addr: "10.0.0.2", Port: 1, Fingerprint: "a", Tool: "codex"},
		{Addr: "10.0.0.2", Port: 1, Fingerprint: "a", Tool: "codex"}, // dup
		{Addr: "10.0.0.3", Port: 1, Fingerprint: "b", Tool: "claude"},
	}
	if got := dedupeBeacons(in, ""); len(got) != 2 {
		t.Fatalf("no-filter dedupe = %d, want 2", len(got))
	}
	got := dedupeBeacons(in, "codex")
	if len(got) != 1 || got[0].Fingerprint != "a" {
		t.Fatalf("tool-filtered dedupe = %+v", got)
	}
}

func TestRememberedBeaconsFilters(t *testing.T) {
	dir := t.TempDir()
	// Remember peer with fingerprint "keep"; "stranger" is unknown.
	if err := rememberPeer(dir, "B", [32]byte{0xab}); err != nil {
		t.Fatal(err)
	}
	keepFP := fpHex([32]byte{0xab})
	beacons := []Beacon{
		{Hostname: "known", Addr: "10.0.0.2", Port: 1, Fingerprint: keepFP},
		{Hostname: "stranger", Addr: "10.0.0.3", Port: 1, Fingerprint: fpHex([32]byte{0x01})},
		{Hostname: "me", Addr: "10.0.0.4", Port: 1, Fingerprint: "myfp"},
	}
	got := rememberedBeacons(beacons, dir, "myfp")
	if len(got) != 1 || got[0].Hostname != "known" {
		t.Fatalf("rememberedBeacons = %+v", got)
	}
}
