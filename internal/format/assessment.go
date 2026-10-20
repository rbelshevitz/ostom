package format

import "fmt"

// IntegrityAssessment is a calm, file-level interpretation of structural
// evidence. It does not diagnose the storage device or claim that unreadable
// bytes were caused by bad sectors.
type IntegrityAssessment struct {
	Classification  string   `json:"classification"`
	DeepRecovery    string   `json:"deep_recovery"`
	RecoveryOutlook string   `json:"recovery_outlook"`
	Confidence      string   `json:"confidence"`
	Summary         string   `json:"summary"`
	Reasons         []string `json:"reasons,omitempty"`
	Warnings        []string `json:"warnings,omitempty"`
}

// AssessFile turns header, allocation, and index-root evidence into a
// conservative recovery recommendation. Advisory CRC/trailer anomalies are
// warnings until stronger evidence agrees with them.
func AssessFile(h Header, roots IndexRootScan, fileSize int64) IntegrityAssessment {
	a := IntegrityAssessment{
		Classification:  "healthy",
		DeepRecovery:    "not_needed",
		RecoveryOutlook: "good",
		Confidence:      "medium",
	}
	if !h.OK {
		return assessment("unknown", "required", "high", "limited", "Файл не распознан как поддерживаемый OST/PST.", "header is not valid")
	}

	actual := uint64(0)
	if fileSize > 0 {
		actual = uint64(fileSize)
	}
	if h.DeclaredSize > 0 && actual > 0 && actual < h.DeclaredSize {
		a.Classification = "degraded"
		a.DeepRecovery = "required"
		a.Confidence = "high"
		a.Reasons = append(a.Reasons, fmt.Sprintf("actual file size is smaller than declared size (%d < %d)", actual, h.DeclaredSize))
	}
	if h.HeaderCRC == "invalid" {
		a.Warnings = append(a.Warnings, "header CRC is invalid")
	}
	if h.Allocation.PagesInvalid > 0 {
		a.Warnings = append(a.Warnings, fmt.Sprintf("%d allocation pages failed validation", h.Allocation.PagesInvalid))
	}

	rootFailures := 0
	for _, root := range []struct {
		name   string
		status string
	}{{"NBT", roots.NBT}, {"BBT", roots.BBT}} {
		name, status := root.name, root.status
		if status == "" || status == "valid" || status == "not applicable" || status == "CRC invalid" {
			if status == "CRC invalid" {
				a.Warnings = append(a.Warnings, name+" root CRC is invalid")
			}
			continue
		}
		rootFailures++
	}
	if rootFailures > 0 {
		a.Classification = "degraded"
		a.Confidence = "high"
		a.Reasons = append(a.Reasons, fmt.Sprintf("%d index roots are not fully readable or valid", rootFailures))
		if roots.NBT == "unreadable" || roots.BBT == "unreadable" || roots.NBT == "outside file" || roots.BBT == "outside file" {
			a.DeepRecovery = maxRecovery(a.DeepRecovery, "required")
		} else {
			a.DeepRecovery = maxRecovery(a.DeepRecovery, "recommended")
		}
	}
	if roots.BadRefs > 0 {
		a.Classification = "degraded"
		a.DeepRecovery = maxRecovery(a.DeepRecovery, "recommended")
		a.Reasons = append(a.Reasons, fmt.Sprintf("%d index references are outside the file", roots.BadRefs))
	}

	if a.Classification == "healthy" && len(a.Warnings) == 0 {
		a.Summary = "Файл выглядит структурно читаемым; обычного восстановления должно быть достаточно."
	} else if a.Classification == "healthy" {
		a.Summary = "Файл читается; обнаружены отдельные технические предупреждения без достаточных оснований считать восстановление обязательным."
	} else if a.DeepRecovery == "required" {
		a.Summary = "Файл читается частично; рекомендуется дополнительное восстановление доступных данных."
	} else {
		a.Summary = "Обнаружены отдельные структурные предупреждения; дополнительное восстановление может найти больше данных."
	}
	if a.DeepRecovery == "required" {
		a.RecoveryOutlook = "limited"
	} else if a.Classification != "healthy" || len(a.Warnings) > 0 {
		a.RecoveryOutlook = "uncertain"
	}
	return a
}

func assessment(classification, recovery, confidence, outlook, summary, reason string) IntegrityAssessment {
	a := IntegrityAssessment{Classification: classification, DeepRecovery: recovery, RecoveryOutlook: outlook, Confidence: confidence, Summary: summary}
	if reason != "" {
		a.Reasons = []string{reason}
	}
	return a
}

func maxRecovery(left, right string) string {
	rank := map[string]int{"not_needed": 0, "recommended": 1, "required": 2}
	if rank[right] > rank[left] {
		return right
	}
	return left
}
