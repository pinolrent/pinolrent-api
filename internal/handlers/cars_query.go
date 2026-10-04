package handlers

import (
	"database/sql"

	"github.com/pinolrent/pinolrent-api/internal/models"
)

const carColumns = "id, owner_id, name, photo_url, price_per_day, active"

const carColumnsQualified = "c.id, c.owner_id, c.name, c.photo_url, c.price_per_day, c.active"

// scanCar reads one row of carColumns into c. It takes the rowScanner
// interface so the single-row and multi-row paths share the field order.
func scanCar(row rowScanner, c *models.Car) error {
	var active int
	if err := row.Scan(&c.ID, &c.OwnerID, &c.Name, &c.PhotoURL, &c.PricePerDay, &active); err != nil {
		return err
	}
	c.Active = active == 1
	return nil
}

func scanCars(rows *sql.Rows) ([]models.Car, error) {
	defer func() { _ = rows.Close() }()
	var cars []models.Car
	for rows.Next() {
		var c models.Car
		if err := scanCar(rows, &c); err != nil {
			return nil, err
		}
		cars = append(cars, c)
	}
	return cars, rows.Err()
}
