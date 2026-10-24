package format

import "testing"

func TestAssessFileRequiresRecoveryWhenHeaderRootsAreMissing(t *testing.T) {
	assessment := AssessFile(Header{OK: true, Roots: "missing NBT/BBT root"}, IndexRootScan{Status: "not applicable"}, 1024)
	if assessment.Classification != "degraded" || assessment.Integrity != "damaged" || assessment.DeepRecovery != "required" {
		t.Fatalf("missing roots assessment = %+v", assessment)
	}
}

func TestAssessFileKeepsAdvisoryCRCAsWarning(t *testing.T) {
	assessment := AssessFile(Header{OK: true, HeaderCRC: "invalid"}, IndexRootScan{NBT: "valid", BBT: "valid"}, 100)
	if assessment.Classification != "healthy" || assessment.Integrity != "warning" || assessment.DeepRecovery != "not_needed" || assessment.RecoveryOutlook != "uncertain" || len(assessment.Warnings) != 1 {
		t.Fatalf("unexpected assessment: %+v", assessment)
	}
	if assessment.Summary == "" || assessment.Summary == "Файл исправен: структура читается согласованно, обязательное спасательное восстановление не требуется." {
		t.Fatalf("warning assessment was presented as healthy: %q", assessment.Summary)
	}
}

func TestAssessFileKeepsRootCRCAsWarning(t *testing.T) {
	assessment := AssessFile(Header{OK: true}, IndexRootScan{NBT: "CRC invalid", BBT: "CRC invalid"}, 100)
	if assessment.Classification != "healthy" || assessment.Integrity != "warning" || assessment.DeepRecovery != "not_needed" || assessment.RecoveryOutlook != "uncertain" {
		t.Fatalf("unexpected assessment: %+v", assessment)
	}
}

func TestAssessFileRequiresRecoveryForTruncatedStore(t *testing.T) {
	assessment := AssessFile(Header{OK: true, DeclaredSize: 200}, IndexRootScan{NBT: "valid", BBT: "valid"}, 100)
	if assessment.Classification != "degraded" || assessment.Integrity != "damaged" || assessment.DeepRecovery != "required" || assessment.RecoveryOutlook != "limited" || assessment.Confidence != "high" {
		t.Fatalf("unexpected assessment: %+v", assessment)
	}
}

func TestAssessFileDoesNotEscalateHealthyStore(t *testing.T) {
	assessment := AssessFile(Header{OK: true}, IndexRootScan{NBT: "valid", BBT: "valid"}, 100)
	if assessment.Classification != "healthy" || assessment.Integrity != "healthy" || assessment.DeepRecovery != "not_needed" || assessment.RecoveryOutlook != "good" {
		t.Fatalf("unexpected assessment: %+v", assessment)
	}
	if assessment.Summary != "Файл исправен: структура читается согласованно, обязательное спасательное восстановление не требуется." {
		t.Fatalf("healthy summary = %q", assessment.Summary)
	}
	if assessment.SummaryEnglish == "" {
		t.Fatal("healthy assessment has no English summary")
	}
}
