// Package models defines the domain types exposed by the API.
package models

// User is an account that can authenticate as a buyer or as a seller.
type User struct {
	ID           int64  `json:"id"`
	Email        string `json:"email"`
	PasswordHash string `json:"-"`
	Role         string `json:"role"`
}

// Car is a rentable vehicle in the catalog, owned by a seller account.
type Car struct {
	ID          int64  `json:"id"`
	OwnerID     int64  `json:"owner_id"`
	Name        string `json:"name"`
	PhotoURL    string `json:"photo_url,omitempty"`
	PricePerDay int64  `json:"price_per_day"`
	Active      bool   `json:"active"`
}

// Reservation is a booking for a car over a date range.
type Reservation struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"user_id"`
	CarID     int64  `json:"car_id"`
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
	Status    string `json:"status"`
}

// Payment is the record of payment for a reservation.
type Payment struct {
	ID            int64  `json:"id"`
	ReservationID int64  `json:"reservation_id"`
	Method        string `json:"method"`
	Status        string `json:"status"`
	ProofURL      string `json:"proof_url,omitempty"`
}
