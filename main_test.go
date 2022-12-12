package main

import "testing"

func Test_Translate_xhtml(t *testing.T) {

	ret := Translate_xhtml("ch01.xhtml", "", "EN", "ZH")

	if ret != true {
		t.Error("Translate_xhtml error")
	} else {
		t.Log("test pass")
	}
}
