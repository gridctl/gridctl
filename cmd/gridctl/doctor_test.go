package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestSummarizeDoctor(t *testing.T) {
	checks := []doctorCheck{
		{ID: "a", Status: doctorStatusOK},
		{ID: "b", Status: doctorStatusWarn},
		{ID: "c", Status: doctorStatusFail},
		{ID: "d", Status: doctorStatusInfo},
		{ID: "e", Status: doctorStatusWarn},
	}
	report := summarizeDoctor(checks)

	if report.ErrorCount != 1 {
		t.Errorf("ErrorCount = %d, want 1", report.ErrorCount)
	}
	if report.WarningCount != 2 {
		t.Errorf("WarningCount = %d, want 2", report.WarningCount)
	}
	if report.OK {
		t.Error("OK should be false with a failing check")
	}

	clean := summarizeDoctor([]doctorCheck{{ID: "a", Status: doctorStatusOK}, {ID: "b", Status: doctorStatusWarn}})
	if !clean.OK {
		t.Error("OK should be true with warnings only")
	}
}

func TestRenderDoctorReportJSON(t *testing.T) {
	report := summarizeDoctor([]doctorCheck{
		{ID: "npx", Status: doctorStatusWarn, Message: "not found"},
	})

	var buf bytes.Buffer
	exit := renderDoctorReport(&buf, report, true, false)
	if exit != doctorExitOK {
		t.Errorf("exit = %d, want %d (warnings only)", exit, doctorExitOK)
	}

	var decoded doctorReport
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if !decoded.OK || decoded.WarningCount != 1 || len(decoded.Checks) != 1 {
		t.Errorf("decoded report mismatch: %+v", decoded)
	}
	if strings.Contains(buf.String(), "\033") {
		t.Error("JSON report must not contain ANSI escapes")
	}
}

func TestRenderDoctorReportExitCodes(t *testing.T) {
	failing := summarizeDoctor([]doctorCheck{{ID: "x", Status: doctorStatusFail}})
	var buf bytes.Buffer
	if exit := renderDoctorReport(&buf, failing, false, false); exit != doctorExitErrors {
		t.Errorf("exit = %d, want %d for a failing check", exit, doctorExitErrors)
	}
}

func TestRenderDoctorHumanQuiet(t *testing.T) {
	report := summarizeDoctor([]doctorCheck{
		{ID: "good", Status: doctorStatusOK, Message: "fine"},
		{ID: "bad", Status: doctorStatusFail, Message: "broken"},
	})

	var buf bytes.Buffer
	renderDoctorHuman(&buf, report, true)
	out := buf.String()
	if strings.Contains(out, "good") {
		t.Errorf("quiet mode should hide passing checks, got %q", out)
	}
	if !strings.Contains(out, "bad") {
		t.Errorf("quiet mode should keep failing checks, got %q", out)
	}
	if !strings.Contains(out, "Result: 1 error(s)") {
		t.Errorf("summary line missing, got %q", out)
	}
}

func TestDoctorStatusLabelPlainWhenNoColor(t *testing.T) {
	label := doctorStatusLabel(doctorStatusFail, false)
	if label != "fail" {
		t.Errorf("label = %q, want padded plain word", label)
	}
	if strings.Contains(label, "\033") {
		t.Error("label must be colorless when color is off")
	}
}
