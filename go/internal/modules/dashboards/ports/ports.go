// Package ports declares what the dashboards module needs.
package ports

import (
	"context"

	"github.com/Mutiur03/pulse/internal/modules/dashboards/domain"
)

// Repository keeps dashboards. Get and List only return dashboards the
// user can see.
type Repository interface {
	List(ctx context.Context, project, user int64) ([]domain.Dashboard, error)
	Get(ctx context.Context, project, id, user int64) (domain.Dashboard, error)
	Create(ctx context.Context, d domain.Dashboard) (domain.Dashboard, error)
	Update(ctx context.Context, d domain.Dashboard, user int64) (domain.Dashboard, error)
	Delete(ctx context.Context, project, id int64) error
}
