package constants

type FreezeStatus string

const (
	FreezePendingApproval FreezeStatus = "pending_approval"
	FreezeActive          FreezeStatus = "active"
	FreezeRejected        FreezeStatus = "rejected"
	FreezeReleased        FreezeStatus = "released"
)

var FreezeStatuses = []FreezeStatus{
	FreezePendingApproval, FreezeActive, FreezeRejected, FreezeReleased,
}

func ValidFreezeStatus(value FreezeStatus) bool {
	for _, status := range FreezeStatuses {
		if status == value {
			return true
		}
	}
	return false
}
