const express = require("express");
const session = require("express-session");
const bcrypt = require("bcrypt");
const path = require("path");

require("dotenv").config();

const pool = require("./db");
const adminRoutes = require("./routes/admin");
const app = express();

const {
    createOtpChallenge,
    verifyOtpChallenge,
    canResendOtp,
    resendOtp
} = require("./services/otp");

const redisClient = require("./redis");

//Middleware
app.use(express.json());
app.use(
    session({
        secret: process.env.SESSION_SECRET,
        resave: false,
        saveUninitialized: false,

        cookie: {
            httpOnly: true,
            secure: false,
            sameSite: "lax"
        }
    })
);


// Login + OTP verification routes
app.post("/api/login", async (req, res) => {

    const { email, password } = req.body;

    if (!email || !password) {
        return res.status(400).json({
            error: "Email and password are required"
        });
    }

    try {

        const result = await pool.query(
            `
            SELECT
                id,
                email,
                password_hash,
                role,
                is_verified
            FROM evoting.users
            WHERE email = $1
            `,
            [email]
        );

        if (result.rows.length === 0) {
            return res.status(401).json({
                error: "Invalid email or password"
            });
        }

        const user = result.rows[0];

        const passwordCorrect = await bcrypt.compare(
            password,
            user.password_hash
        );

        if (!passwordCorrect) {
            return res.status(401).json({
                error: "Invalid email or password"
            });
        }

        if (!user.is_verified) {
            return res.status(403).json({
                error: "Account is not verified"
            });
        }


        // Store only necessary information in session
        const { challengeId, otp } =
            await createOtpChallenge(user.id);

        req.session.pendingLogin = {
            challengeId
        };

        if (process.env.NODE_ENV !== "production") {
            console.log(
                `[DEV ONLY] OTP for ${user.email}: ${otp}`
            );
        }

        req.session.save((error) => {
            if (error) {
                console.error(error);

                return res.status(500).json({
                    error: "Failed to create login session"
                });
            }

            res.json({
                message: "OTP required",
                otp_required: true
            });
        });

    } catch (error) {

        console.error(error);

        res.status(500).json({
            error: "Login failed"
        });
    }
});

app.post("/api/login/verify-otp", async (req, res) => {
    const { otp } = req.body;

    if (!otp || !/^\d{6}$/.test(otp)) {
        return res.status(400).json({
            error: "OTP must be 6 digits"
        });
    }

    const challengeId =
        req.session.pendingLogin?.challengeId;

    if (!challengeId) {
        return res.status(401).json({
            error: "No OTP challenge found"
        });
    }

    try {
        const result = await verifyOtpChallenge(
            challengeId,
            otp
        );

        if (!result.success) {
            if (result.reason === "too_many_attempts") {
                delete req.session.pendingLogin;

                return res.status(429).json({
                    error: "Too many OTP attempts"
                });
            }

            if (result.reason === "expired_or_invalid") {
                delete req.session.pendingLogin;

                return res.status(401).json({
                    error: "OTP expired or invalid"
                });
            }

            return res.status(401).json({
                error: "Invalid OTP"
            });
        }

        const userResult = await pool.query(
            `
            SELECT
                id,
                email,
                role,
                is_verified
            FROM evoting.users
            WHERE id = $1
            `,
            [result.userId]
        );

        if (userResult.rows.length === 0) {
            delete req.session.pendingLogin;

            return res.status(401).json({
                error: "User account not found"
            });
        }

        const user = userResult.rows[0];

        if (!user.is_verified) {
            delete req.session.pendingLogin;

            return res.status(403).json({
                error: "Account is not verified"
            });
        }

        req.session.regenerate((error) => {
            if (error) {
                console.error(error);

                return res.status(500).json({
                    error: "Failed to create authenticated session"
                });
            }

            req.session.user = {
                id: user.id,
                email: user.email,
                role: user.role
            };

            res.json({
                message: "Authentication successful",
                user: {
                    email: user.email,
                    role: user.role
                }
            });
        });

    } catch (error) {
        console.error(error);

        res.status(500).json({
            error: "OTP verification failed"
        });
    }
});


app.post("/api/login/resend-otp", async (req, res) => {
    const challengeId =
        req.session.pendingLogin?.challengeId;

    if (!challengeId) {
        return res.status(401).json({
            error: "No OTP challenge found"
        });
    }

    try {
        const allowed =
            await canResendOtp(challengeId);

        if (!allowed) {
            return res.status(429).json({
                error: "Please wait before requesting another OTP"
            });
        }

        const result =
            await resendOtp(challengeId);

        if (!result) {
            delete req.session.pendingLogin;

            return res.status(401).json({
                error: "OTP challenge expired"
            });
        }

        // Development only.
        if (process.env.NODE_ENV !== "production") {
            console.log(
                `[DEV ONLY] Resent OTP: ${result.otp}`
            );
        }

        res.json({
            message: "OTP resent"
        });

    } catch (error) {
        console.error(error);

        res.status(500).json({
            error: "Failed to resend OTP"
        });
    }
});

// =============================
// Get current logged-in user
// =============================

app.get("/api/me", (req, res) => {

    if (!req.session.user) {
        return res.status(401).json({
            error: "Not authenticated"
        });
    }

    res.json({
        user: req.session.user
    });
});


// =============================
// Logout
// =============================

app.post("/api/logout", (req, res) => {

    req.session.destroy((error) => {

        if (error) {
            return res.status(500).json({
                error: "Logout failed"
            });
        }

        res.json({
            message: "Logged out"
        });
    });
});


// =============================
// Admin routes
// =============================

app.use("/api/admin", adminRoutes);


// =============================
// Frontend
// =============================

app.use(express.static(
    path.join(__dirname, "../frontend")
));


app.get("/api/redis-test", async (req, res) => {
    try {
        await redisClient.set("evoting:test", "redis-is-working", {
            EX: 60
        });

        const value = await redisClient.get("evoting:test");

        res.json({
            redis: value
        });
    } catch (error) {
        console.error(error);

        res.status(500).json({
            error: "Redis test failed"
        });
    }
});

// =============================
// Start server
// =============================

const PORT = process.env.PORT || 3000;

async function startServer() {
    try {
        await redisClient.connect();

        console.log("Redis connected");

        app.listen(PORT, () => {
            console.log(`Server running at http://localhost:${PORT}`);
        });
    } catch (error) {
        console.error("Failed to start server:", error);
        process.exit(1);
    }
}

startServer();