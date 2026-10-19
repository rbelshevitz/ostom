package format

import "testing"

func TestAssessFileKeepsAdvisoryCRCAsWarning(t *testing.T) {
	assessment := AssessFile(Header{OK: true, HeaderCRC: "invalid"}, IndexRootScan{NBT: "valid", BBT: "valid"}, 100)
	if assessment.Classification != "healthy" || assessment.DeepRecovery != "not_needed" || len(assessment.Warnings) != 1 {
		t.Fatalf("unexpected assessment: %+v", assessment)
	}
}

func TestAssessFileKeepsRootCRCAsWarning(t *testing.T) {
	assessment := AssessFile(Header{OK: true}, IndexRootScan{NBT: "CRC invalid", BBT: "CRC invalid"}, 100)
	if assessment.Classification != "healthy" || assessment.DeepRecovery != "not_needed" {
		t.Fatalf("unexpected assessment: %+v", assessment)
	}
}

func TestAssessFileRequiresRecoveryForTruncatedStore(t *testing.T) {
	assessment := AssessFile(Header{OK: true, DeclaredSize: 200}, IndexRootScan{NBT: "valid", BBT: "valid"}, 100)
	if assessment.Classification != "degraded" || assessment.DeepRecovery != "required" || assessment.Confidence != "high" {
		t.Fatalf("unexpected assessment: %+v", assessment)
	}
}

func TestAssessFileDoesNotEscalateHealthyStore(t *testing.T) {
	assessment := AssessFile(Header{OK: true}, IndexRootScan{NBT: "valid", BBT: "valid"}, 100)
	if assessment.Classification != "healthy" || assessment.DeepRecovery != "not_needed" {
		t.Fatalf("unexpected assessment: %+v", assessment)
	}
}
