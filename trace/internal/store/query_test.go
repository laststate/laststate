package store

import "testing"

func TestParseEventQuery(t *testing.T) {
	q := ParseEventQuery(`severity:fatal device:sensor has:stack age:<24h sample:0.5 boom`)
	if q.Severity != "fatal" || q.Device != "sensor" || !q.HasStack || q.Age == 0 || q.SampleRate != 0.5 {
		t.Fatalf("%+v", q)
	}
	if q.Text != "boom" {
		t.Fatal(q.Text)
	}
}
