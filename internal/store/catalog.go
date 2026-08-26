package store

import (
	"context"
	"fmt"
	"time"

	"github.com/11DingKing/robotcell-lifecycle-control/internal/maintenance"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/schedule"
)

func (s *Store) CreateWorkstation(ctx context.Context, station schedule.Workstation) (schedule.Workstation, error) {
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `INSERT INTO workstations(code,name,line,active,version,created_at) VALUES(?,?,?,?,1,?)`, station.Code, station.Name, station.Line, boolInt(station.Active), encodeTime(now))
	if err != nil {
		return schedule.Workstation{}, fmt.Errorf("create workstation: %w", err)
	}
	station.ID, err = result.LastInsertId()
	station.Version = 1
	station.CreatedAt = now
	return station, err
}

func (s *Store) CreateTool(ctx context.Context, tool schedule.Tool) (schedule.Tool, error) {
	result, err := s.db.ExecContext(ctx, `INSERT INTO tools(code,name,calibration_due,active,version) VALUES(?,?,?,?,1)`, tool.Code, tool.Name, encodeTime(tool.CalibrationDue), boolInt(tool.Active))
	if err != nil {
		return schedule.Tool{}, fmt.Errorf("create tool: %w", err)
	}
	tool.ID, err = result.LastInsertId()
	tool.Version = 1
	return tool, err
}

func (s *Store) CreateQualification(ctx context.Context, item schedule.Qualification) (schedule.Qualification, error) {
	result, err := s.db.ExecContext(ctx, `INSERT INTO qualifications(user_id,kind,valid_from,valid_to,revoked_at) VALUES(?,?,?,?,NULL)`, item.UserID, item.Kind, encodeTime(item.ValidFrom), encodeTime(item.ValidTo))
	if err != nil {
		return schedule.Qualification{}, fmt.Errorf("create qualification: %w", err)
	}
	item.ID, err = result.LastInsertId()
	return item, err
}

func (s *Store) CreateSparePart(ctx context.Context, part maintenance.SparePart) (maintenance.SparePart, error) {
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `INSERT INTO spare_parts(code,name,available,reserved,version,updated_at) VALUES(?,?,?,0,1,?)`, part.Code, part.Name, part.Available, encodeTime(now))
	if err != nil {
		return maintenance.SparePart{}, fmt.Errorf("create spare part: %w", err)
	}
	part.ID, err = result.LastInsertId()
	part.Reserved = 0
	part.Version = 1
	part.UpdatedAt = now
	return part, err
}
