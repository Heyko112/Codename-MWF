package tables

import "testing"

func TestSQLType(t *testing.T) {
	cases := map[string]string{
		sqlType("text", 80):                   "VARCHAR(80)",
		sqlType("decimal", 2):                 "DECIMAL(20,2)",
		sqlType("bool", 0):                    "BOOLEAN",
		sqlType("datetime", 0):                "DATETIME",
		tableIdent(7) + "." + columnIdent(12): "`dt_7`.`c_12`",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("%q, erwartet %q", got, want)
		}
	}
	for _, typ := range Types {
		sqlType(typ.Code, typ.DefaultSize) // darf für keinen Typ paniken
	}
}

func TestCheckSize(t *testing.T) {
	text := TypeByCode("text")
	if _, err := CheckSize(text, 0); err == nil {
		t.Error("Länge 0 bei Text akzeptiert")
	}
	if _, err := CheckSize(text, 1001); err == nil {
		t.Error("Länge 1001 bei Text akzeptiert")
	}
	if n, err := CheckSize(TypeByCode("int"), 99); err != nil || n != 0 {
		t.Errorf("Ganzzahl: %d, %v", n, err)
	}
}
