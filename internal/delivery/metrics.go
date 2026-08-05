package delivery

type Metrics struct {
	AuthorisedTotal         int64 `json:"authorisedTotal"`
	QueuedTotal             int64 `json:"queuedTotal"`
	SubmittedTotal          int64 `json:"submittedTotal"`
	SentTotal               int64 `json:"sentTotal"`
	DeliveredTotal          int64 `json:"deliveredTotal"`
	ReadTotal               int64 `json:"readTotal"`
	FailedTotal             int64 `json:"failedTotal"`
	UnknownTotal            int64 `json:"unknownTotal"`
	SuppressedTotal         int64 `json:"suppressedTotal"`
	ExcludedFinalCheckTotal int64 `json:"excludedFinalCheckTotal"`
}

func (m Metrics) Move(previous, current Status) Metrics {
	if previous != "" {
		m.add(previous, -1)
	}
	m.add(current, 1)
	return m
}

func (m *Metrics) add(status Status, delta int64) {
	switch status {
	case StatusAuthorised:
		m.AuthorisedTotal += delta
	case StatusQueued, StatusClaimed:
		m.QueuedTotal += delta
	case StatusSubmitting, StatusGatewayAccepted:
		m.SubmittedTotal += delta
	case StatusSent:
		m.SentTotal += delta
	case StatusDelivered:
		m.DeliveredTotal += delta
	case StatusRead:
		m.ReadTotal += delta
	case StatusFailedRetryable, StatusFailedPermanent:
		m.FailedTotal += delta
	case StatusUnknown:
		m.UnknownTotal += delta
	case StatusSuppressedBeforeSend:
		m.SuppressedTotal += delta
	}
}

// Delta returns the signed counter adjustments required when a recipient moves between
// canonical delivery states. A changed event that does not change state yields zero.
func Delta(previous, current Status) Metrics {
	var delta Metrics
	if previous == current {
		return delta
	}
	if previous != "" {
		delta.add(previous, -1)
	}
	if current != "" {
		delta.add(current, 1)
	}
	return delta
}
