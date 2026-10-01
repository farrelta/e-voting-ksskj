const express = require("express");
const session = require("express-session");
const bcrypt = require("bcrypt");
const path = require("path");

require("dotenv").config();

const pool = require("./db");
const adminRoutes = require("./routes/admin");
const app = express();


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


// Login
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
        req.session.user = {
            id: user.id,
            email: user.email,
            role: user.role
        };


        res.json({
            message: "Login successful",
            user: {
                email: user.email,
                role: user.role
            }
        });

    } catch (error) {

        console.error(error);

        res.status(500).json({
            error: "Login failed"
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


// =============================
// Start server
// =============================

const PORT = process.env.PORT || 3000;

app.listen(PORT, () => {
    console.log(`Server running at http://localhost:${PORT}`);
});