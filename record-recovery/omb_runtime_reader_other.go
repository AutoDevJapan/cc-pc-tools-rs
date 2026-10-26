//go:build !windows

package main

func omb3255AcquireRuntime() omb3255RuntimeAcquisition {
	return omb3255RuntimeAcquisition{state: "platform_unavailable"}
}

func omb3255AcquireAdmissionRuntime() omb3255RuntimeAcquisition {
	return omb3255RuntimeAcquisition{state: "platform_unavailable"}
}
func acquireUnstartedReviewRuntime() omb3255RuntimeAcquisition {
	return omb3255RuntimeAcquisition{state: "platform_unavailable"}
}
func acquirePrivateReviewRuntime() omb3255RuntimeAcquisition {
	return omb3255RuntimeAcquisition{state: "platform_unavailable"}
}

func acquireConsumerReviewRuntime() omb3255RuntimeAcquisition {
	return omb3255RuntimeAcquisition{state: "platform_unavailable"}
}
