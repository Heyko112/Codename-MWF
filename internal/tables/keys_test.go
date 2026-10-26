package tables

import "testing"

func TestMakeKey(t *testing.T) {
	cases := map[string]string{
		"Firma":          "firma",
		"Größe (cm)":     "groesse_cm",
		"  Straße & Nr ": "strasse_nr",
		"2. Adresse":     "f_2_adresse",
		"€€€":            "f_",
	}
	for in, want := range cases {
		if got := MakeKey(in); got != want {
			t.Errorf("%q → %q, erwartet %q", in, got, want)
		}
	}
	if k := uniqueKey("firma", map[string]bool{"firma": true, "firma_2": true}); k != "firma_3" {
		t.Error(k)
	}
	if k := uniqueKey("id", nil); k != "id_feld" {
		t.Error(k)
	}
}
