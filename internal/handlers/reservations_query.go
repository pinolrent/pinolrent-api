package handlers

import (
	"context"
	"database/sql"

	"github.com/pinolrent/pinolrent-api/internal/models"
)

type reservationView struct {
	models.Reservation
	Car     *models.Car     `json:"car"`
	Payment *models.Payment `json:"payment,omitempty"`
}

// reservationSelect lists the columns scanReservation expects: the reservation
// itself, its car, and an optional payment from the LEFT JOIN. The car block is
// built from carColumnsQualified so adding a car column cannot desync the JOIN.
const reservationSelect = `
SELECT r.id, r.user_id, r.car_id, r.start_date, r.end_date, r.status,
	` + carColumnsQualified + `,
	p.id, p.reservation_id, p.method, p.status, p.proof_url
FROM reservations r
JOIN cars c ON c.id = r.car_id
LEFT JOIN payments p ON p.reservation_id = r.id
`

func scanReservation(row rowScanner, v *reservationView) error {
	var c models.Car
	var p models.Payment
	var active int
	var pID, pResID sql.NullInt64
	var pMethod, pStatus, pProof sql.NullString

	err := row.Scan(
		&v.ID, &v.UserID, &v.CarID, &v.StartDate, &v.EndDate, &v.Status,
		&c.ID, &c.OwnerID, &c.Name, &c.PhotoURL, &c.PricePerDay, &active,
		&pID, &pResID, &pMethod, &pStatus, &pProof,
	)
	if err != nil {
		return err
	}

	c.Active = active == 1
	v.Car = &c
	if pID.Valid {
		p.ID = pID.Int64
		p.ReservationID = pResID.Int64
		p.Method = pMethod.String
		p.Status = pStatus.String
		p.ProofURL = pProof.String
		v.Payment = &p
	}
	return nil
}

func (a *API) reservationView(ctx context.Context, id int64) (reservationView, error) {
	var v reservationView
	err := scanReservation(a.DB.QueryRowContext(ctx, reservationSelect+` WHERE r.id = ?`, id), &v)
	return v, err
}
