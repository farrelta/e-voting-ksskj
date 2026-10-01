

const express = require("express");
const pool = require("../db");
const { requireAdmin } = require("../middleware/auth");

const router = express.Router();


//mengirim sinyal: GET /api/admin/elections/:electionId/voters
router.get(
    "/elections/:electionId/voters",
    requireAdmin,
    async (req, res) => {

        const { electionId } = req.params;

        try {

            const result = await pool.query(
                `
                SELECT
                    u.id AS user_id,
                    u.email,
                    ve.has_voted
                FROM evoting.voter_eligibility ve
                JOIN evoting.users u
                    ON ve.user_id = u.id
                WHERE ve.election_id = $1
                ORDER BY u.email;
                `,
                [electionId]
            );

            res.json({
                election_id: electionId,
                voters: result.rows
            });

        } catch (error) {

            console.error(error);

            res.status(500).json({
                error: "Failed to retrieve voter status"
            });
        }
    }
);


module.exports = router;