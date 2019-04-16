package sentry

import (
	"reflect"
	"testing"
)

var input = [][]byte{
	[]byte("line 1"),
	[]byte("line 2"),
	[]byte("line 3"),
}

func assertContextLines(t *testing.T, gotLines, wantLines [][]byte, gotReadLines, wantReadLines int) {
	t.Helper()

	if !reflect.DeepEqual(gotLines, wantLines) {
		t.Errorf("incorrect context lines returned. got %s, want %s", gotLines, wantLines)
	}

	if gotReadLines != wantReadLines {
		t.Errorf("incorrect context lines count returned. got %d, want %d", gotReadLines, wantReadLines)
	}
}

func TestCalculateContextLinesSingleLine(t *testing.T) {
	gotLines, gotReadLines := calculateContextLines(input, 2, 0)
	wantLines, wantReadLines := [][]byte{
		[]byte("line 2"),
	}, 1

	assertContextLines(t, gotLines, wantLines, gotReadLines, wantReadLines)
}

func TestCalculateContextLinesNegativeLine(t *testing.T) {
	gotLines, gotReadLines := calculateContextLines(input, -2, 0)
	var wantLines [][]byte
	var wantReadLines int

	assertContextLines(t, gotLines, wantLines, gotReadLines, wantReadLines)
}

func TestCalculateContextLinesOverflowLine(t *testing.T) {
	gotLines, gotReadLines := calculateContextLines(input, 10, 0)
	var wantLines [][]byte
	var wantReadLines int

	assertContextLines(t, gotLines, wantLines, gotReadLines, wantReadLines)
}

func TestCalculateContextLinesWholeFile(t *testing.T) {
	gotLines, gotReadLines := calculateContextLines(input, 2, 1)
	wantLines, wantReadLines := [][]byte{
		[]byte("line 1"),
		[]byte("line 2"),
		[]byte("line 3"),
	}, 3

	assertContextLines(t, gotLines, wantLines, gotReadLines, wantReadLines)
}

func TestCalculateContextLinesOverflowContextAtTheTop(t *testing.T) {
	gotLines, gotReadLines := calculateContextLines(input, 1, 1)
	wantLines, wantReadLines := [][]byte{
		[]byte("line 1"),
		[]byte("line 2"),
	}, 2

	assertContextLines(t, gotLines, wantLines, gotReadLines, wantReadLines)
}

func TestCalculateContextLinesOverflowContextAtTheBottom(t *testing.T) {
	gotLines, gotReadLines := calculateContextLines(input, 3, 1)
	wantLines, wantReadLines := [][]byte{
		[]byte("line 2"),
		[]byte("line 3"),
	}, 2

	assertContextLines(t, gotLines, wantLines, gotReadLines, wantReadLines)
}

func TestCalculateContextLinesOverflowContextBothSides(t *testing.T) {
	gotLines, gotReadLines := calculateContextLines(input, 2, 2)
	wantLines, wantReadLines := [][]byte{
		[]byte("line 1"),
		[]byte("line 2"),
		[]byte("line 3"),
	}, 3

	assertContextLines(t, gotLines, wantLines, gotReadLines, wantReadLines)
}

func TestCalculateContextLinesNegativeContext(t *testing.T) {
	gotLines, gotReadLines := calculateContextLines(input, 2, -1)
	wantLines, wantReadLines := [][]byte{
		[]byte("line 2"),
	}, 1

	assertContextLines(t, gotLines, wantLines, gotReadLines, wantReadLines)
}
