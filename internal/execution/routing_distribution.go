package execution

func prepareDistribution(plan *RoutingPlan) error {
	if plan == nil {
		return ErrRoutingPlanInvalid
	}
	if plan.DistributionMode == "" {
		plan.DistributionMode = DistributionWeighted
	}
	if plan.DistributionMode != DistributionAuto {
		return nil
	}
	maxRate := 0
	for _, route := range plan.Routes {
		if route.ReservedMessagesPerMinute <= 0 {
			return ErrRoutingPlanInvalid
		}
		if route.ReservedMessagesPerMinute > maxRate {
			maxRate = route.ReservedMessagesPerMinute
		}
	}
	scale := 1
	if maxRate > 10000 {
		scale = (maxRate + 9999) / 10000
	}
	for i := range plan.Routes {
		weight := plan.Routes[i].ReservedMessagesPerMinute / scale
		if weight < 1 {
			weight = 1
		}
		plan.Routes[i].AllocationWeight = weight
	}
	return nil
}
