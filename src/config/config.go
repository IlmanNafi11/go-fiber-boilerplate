package config

import (
	"app/src/utils"
	"strconv"

	"github.com/spf13/viper"
)

var (
	IsProd              bool
	AppHost             string
	AppPort             int
	DBHost              string
	DBUser              string
	DBPassword          string
	DBName              string
	DBPort              int
	JWTSecret           string
	JWTAccessExp        int
	JWTRefreshExp       int
	JWTResetPasswordExp int
	JWTVerifyEmailExp   int
	SMTPHost            string
	SMTPPort            int
	SMTPUsername        string
	SMTPPassword        string
	EmailFrom           string
	GoogleClientID      string
	GoogleClientSecret  string
	RedirectURL         string
	RedisEnabled        bool
	RedisHost           string
	RedisPort           int
	RedisPassword       string
	RedisDB             int
	SessionCacheTTL     int
)

func init() {
	loadConfig()

	// server configuration
	IsProd = viper.GetString("APP_ENV") == "prod"
	AppHost = viper.GetString("APP_HOST")
	AppPort = viper.GetInt("APP_PORT")

	// database configuration
	DBHost = viper.GetString("DB_HOST")
	DBUser = viper.GetString("DB_USER")
	DBPassword = viper.GetString("DB_PASSWORD")
	DBName = viper.GetString("DB_NAME")
	DBPort = viper.GetInt("DB_PORT")

	// jwt configuration
	JWTSecret = viper.GetString("JWT_SECRET")
	JWTAccessExp = viper.GetInt("JWT_ACCESS_EXP_MINUTES")
	JWTRefreshExp = viper.GetInt("JWT_REFRESH_EXP_DAYS")
	JWTResetPasswordExp = viper.GetInt("JWT_RESET_PASSWORD_EXP_MINUTES")
	JWTVerifyEmailExp = viper.GetInt("JWT_VERIFY_EMAIL_EXP_MINUTES")

	// SMTP configuration
	SMTPHost = viper.GetString("SMTP_HOST")
	SMTPPort = viper.GetInt("SMTP_PORT")
	SMTPUsername = viper.GetString("SMTP_USERNAME")
	SMTPPassword = viper.GetString("SMTP_PASSWORD")
	EmailFrom = viper.GetString("EMAIL_FROM")

	// oauth2 configuration
	GoogleClientID = viper.GetString("GOOGLE_CLIENT_ID")
	GoogleClientSecret = viper.GetString("GOOGLE_CLIENT_SECRET")
	RedirectURL = viper.GetString("REDIRECT_URL")

	// redis configuration
	RedisHost = viper.GetString("REDIS_HOST")
	RedisPort = viper.GetInt("REDIS_PORT")
	RedisPassword = viper.GetString("REDIS_PASSWORD")
	RedisDB = viper.GetInt("REDIS_DB")

	// Validate Redis configuration and set RedisEnabled flag
	if err := ValidateRedisConfig(RedisHost, RedisPort, RedisDB); err != nil {
		utils.Log.Fatal(err)
	}

	// Load session cache configuration
	LoadSessionCacheConfig()
}

func loadConfig() {
	configPaths := []string{
		"./",     // For app
		"../../", // For test folder
	}

	for _, path := range configPaths {
		viper.SetConfigFile(path + ".env")

		if err := viper.ReadInConfig(); err == nil {
			utils.Log.Infof("Config file loaded from %s", path)
			return
		}
	}

	utils.Log.Error("Failed to load any config file")
}

// LoadSessionCacheConfig loads session cache TTL configuration from environment
// Default: 30 minutes, Range: 10-120 minutes
func LoadSessionCacheConfig() {
	// Default TTL: 30 minutes
	defaultTTL := 30
	SessionCacheTTL = defaultTTL

	// Read from environment
	sessionTTLStr := viper.GetString("SESSION_CACHE_TTL")
	if sessionTTLStr == "" {
		utils.Log.Infof("Session cache TTL not specified, using default: %d minutes", defaultTTL)
		return
	}

	// Parse to integer
	sessionTTL, err := strconv.Atoi(sessionTTLStr)
	if err != nil {
		utils.Log.Errorf("Invalid SESSION_CACHE_TTL value '%s': %v. Using default: %d minutes", sessionTTLStr, err, defaultTTL)
		return
	}

	// Validate range (10-120 minutes for security guardrails)
	if sessionTTL < 10 || sessionTTL > 120 {
		utils.Log.Warnf("SESSION_CACHE_TTL value %d minutes is outside allowed range (10-120). Using default: %d minutes", sessionTTL, defaultTTL)
		return
	}

	// Apply valid value
	SessionCacheTTL = sessionTTL
	utils.Log.Infof("Session cache TTL configured: %d minutes", SessionCacheTTL)
}
