package csi

import (
	"context"

	"google.golang.org/grpc/health/grpc_health_v1"
)

type HealthCheck func(ctx context.Context) error

var AlwaysHealthy HealthCheck = func(ctx context.Context) error {
	return nil
}

type healthServer struct {
	grpc_health_v1.UnimplementedHealthServer
	checker HealthCheck
}

func NewHealthServer(c HealthCheck) grpc_health_v1.HealthServer {
	return &healthServer{
		checker: c,
	}
}

func (srv *healthServer) Check(ctx context.Context, _ *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	if err := srv.checker(ctx); err != nil {
		return &grpc_health_v1.HealthCheckResponse{Status: grpc_health_v1.HealthCheckResponse_NOT_SERVING}, nil
	}

	return &grpc_health_v1.HealthCheckResponse{Status: grpc_health_v1.HealthCheckResponse_SERVING}, nil
}
