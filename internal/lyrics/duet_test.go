package lyrics

import "testing"

func TestParseDuetLabelsAndSimultaneousLines(t *testing.T) {
	lines, err := parseLRC("[00:01.00]甲：第一句\n[00:01.00]乙：第二句\n[00:03.00]【合唱】一起唱\n[00:04.00]下一句")
	if err != nil || len(lines) != 4 {
		t.Fatalf("parse failed: %v", err)
	}
	if lines[0].Speaker != "甲" || lines[1].Speaker != "乙" || lines[2].Speaker != "合唱" || lines[3].Speaker != "合唱" {
		t.Fatalf("speaker labels lost: %#v", lines)
	}
	if lines[0].Start != lines[1].Start || lines[0].Text != "第一句" {
		t.Fatal("simultaneous duet timing changed")
	}
}
