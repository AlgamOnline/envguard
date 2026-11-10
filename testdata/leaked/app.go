package main

import "os"

func main() {
	_ = os.Getenv("PORT")
	_ = os.Getenv("STRIPE_KEY")
	_ = os.Getenv("DB_URL")
}
