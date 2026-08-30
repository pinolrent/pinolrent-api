package models

type User struct {
	ID           int64  `json:"id"`
	Email        string `json:"email"`
	PasswordHash string `json:"-"`
	Role         string `json:"role"`
}

type Car struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	PhotoURL    string `json:"photo_url,omitempty"`
	PricePerDay int64  `json:"price_per_day"`
	Active      bool   `json:"active"`
}

type Reservation struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"user_id"`
	CarID     int64  `json:"car_id"`
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
	Status    string `json:"status"`
}

type Payment struct {
	ID            int64  `json:"id"`
	ReservationID int64  `json:"reservation_id"`
	Method        string `json:"method"`
	Status        string `json:"status"`
	ProofURL      string `json:"proof_url,omitempty"`
}