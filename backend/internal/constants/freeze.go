package constants

type FreezeStatus string

const (
	FreezePendingReview FreezeStatus = "pending_review"
	FreezeActive        FreezeStatus = "active"
	FreezeRejected      FreezeStatus = "rejected"
	FreezeReleased      FreezeStatus = "released"
)

var FreezeStatuses = []FreezeStatus{
	FreezePendingReview, FreezeActive, FreezeRejected, FreezeReleased,
}

func ValidFreezeStatus(value FreezeStatus) bool {
	for _, status := range FreezeStatuses {
		if status == value {
			return true
		}
	}
	return false
}

// CanReviewFreeze 限定复核决定只允许发生在待复核状态。
func CanReviewFreeze(from FreezeStatus, to FreezeStatus) bool {
	if from != FreezePendingReview {
		return false
	}
	return to == FreezeActive || to == FreezeRejected
}

// CanReleaseFreeze 只有生效冻结可以解除，解除后不再允许任何迁移。
func CanReleaseFreeze(from FreezeStatus, to FreezeStatus) bool {
	return from == FreezeActive && to == FreezeReleased
}

func FreezeStatusValues() []string {
	values := make([]string, 0, len(FreezeStatuses))
	for _, status := range FreezeStatuses {
		values = append(values, string(status))
	}
	return values
}
