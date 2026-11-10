const { PORT, DB_URL: dbUrl, STRIPE_KEY = "sk_test" } = process.env;
console.log(PORT, dbUrl, STRIPE_KEY);
