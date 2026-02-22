package main

import (
	"context"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
)

func init() {
	orphanCleanupFuncs["app"] = globalisOrphanCleanup
}

func globalisOrphanCleanup(ctx context.Context, pool *pgxpool.Pool) error {
	// SET NULL orphans
	setNullQueries := []string{
		`UPDATE app.bans SET report_id = NULL WHERE report_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM app.reports r WHERE r.report_id = app.bans.report_id)`,
		`UPDATE app.bans SET target_user_identifier = NULL WHERE target_user_identifier IS NOT NULL AND NOT EXISTS (SELECT 1 FROM app.users u WHERE u.identifier = app.bans.target_user_identifier)`,
		`UPDATE app.reports SET chat_identifier = NULL WHERE chat_identifier IS NOT NULL AND NOT EXISTS (SELECT 1 FROM app.chats c WHERE c.identifier = app.reports.chat_identifier)`,
		`UPDATE app.reports SET reporter_user_identifier = NULL WHERE reporter_user_identifier IS NOT NULL AND NOT EXISTS (SELECT 1 FROM app.users u WHERE u.identifier = app.reports.reporter_user_identifier)`,
		`UPDATE app.reports SET target_user_identifier = NULL WHERE target_user_identifier IS NOT NULL AND NOT EXISTS (SELECT 1 FROM app.users u WHERE u.identifier = app.reports.target_user_identifier)`,
		`UPDATE app.users_profile SET avatar_hash = NULL WHERE avatar_hash IS NOT NULL AND NOT EXISTS (SELECT 1 FROM app.images i WHERE i.hash = app.users_profile.avatar_hash)`,
	}

	// DELETE orphans
	deleteQueries := []string{
		`DELETE FROM app.auth_codes WHERE parent_user_identifier IS NOT NULL AND NOT EXISTS (SELECT 1 FROM app.users u WHERE u.identifier = app.auth_codes.parent_user_identifier)`,
		`DELETE FROM app.chat_messages WHERE NOT EXISTS (SELECT 1 FROM app.chats c WHERE c.identifier = app.chat_messages.parent_chat_identifier)`,
		`DELETE FROM app.chat_messages WHERE sender_user_identifier IS NOT NULL AND NOT EXISTS (SELECT 1 FROM app.users u WHERE u.identifier = app.chat_messages.sender_user_identifier)`,
		`DELETE FROM app.chat_participants WHERE NOT EXISTS (SELECT 1 FROM app.chats c WHERE c.identifier = app.chat_participants.parent_chat_identifier)`,
		`DELETE FROM app.chat_participants WHERE NOT EXISTS (SELECT 1 FROM app.users u WHERE u.identifier = app.chat_participants.user_identifier)`,
		`DELETE FROM app.ignores WHERE NOT EXISTS (SELECT 1 FROM app.users u WHERE u.identifier = app.ignores.first_user_identifier)`,
		`DELETE FROM app.ignores WHERE NOT EXISTS (SELECT 1 FROM app.users u WHERE u.identifier = app.ignores.second_user_identifier)`,
		`DELETE FROM app.likes WHERE NOT EXISTS (SELECT 1 FROM app.users u WHERE u.identifier = app.likes.sender_user_identifier)`,
		`DELETE FROM app.likes WHERE NOT EXISTS (SELECT 1 FROM app.users u WHERE u.identifier = app.likes.receiver_user_identifier)`,
		`DELETE FROM app.match_messages WHERE NOT EXISTS (SELECT 1 FROM app.matches m WHERE m.match_id = app.match_messages.match_id)`,
		`DELETE FROM app.match_messages WHERE NOT EXISTS (SELECT 1 FROM app.users u WHERE u.identifier = app.match_messages.sender_user_identifier)`,
		`DELETE FROM app.match_participants WHERE NOT EXISTS (SELECT 1 FROM app.matches m WHERE m.match_id = app.match_participants.parent_match_id)`,
		`DELETE FROM app.match_participants WHERE NOT EXISTS (SELECT 1 FROM app.users u WHERE u.identifier = app.match_participants.user_identifier)`,
		`DELETE FROM app.user_events WHERE NOT EXISTS (SELECT 1 FROM app.users u WHERE u.identifier = app.user_events.parent_user_identifier)`,
		`DELETE FROM app.user_fcm_tokens WHERE NOT EXISTS (SELECT 1 FROM app.users u WHERE u.identifier = app.user_fcm_tokens.parent_user_identifier)`,
		`DELETE FROM app.user_ip_addresses WHERE NOT EXISTS (SELECT 1 FROM app.users u WHERE u.identifier = app.user_ip_addresses.parent_user_identifier)`,
		`DELETE FROM app.user_platform_unique_identifiers WHERE NOT EXISTS (SELECT 1 FROM app.users u WHERE u.identifier = app.user_platform_unique_identifiers.parent_user_identifier)`,
		`DELETE FROM app.user_sessions WHERE NOT EXISTS (SELECT 1 FROM app.users u WHERE u.identifier = app.user_sessions.parent_user_identifier)`,
		`DELETE FROM app.users_geo WHERE NOT EXISTS (SELECT 1 FROM app.users u WHERE u.identifier = app.users_geo.parent_user_identifier)`,
		`DELETE FROM app.users_profile WHERE NOT EXISTS (SELECT 1 FROM app.users u WHERE u.identifier = app.users_profile.parent_user_identifier)`,
		`DELETE FROM app.user_subscriptions WHERE NOT EXISTS (SELECT 1 FROM app.subscriptions s WHERE s.subscription_id = app.user_subscriptions.subscription_id)`,
		`DELETE FROM app.user_subscriptions WHERE NOT EXISTS (SELECT 1 FROM app.users u WHERE u.identifier = app.user_subscriptions.parent_user_identifier)`,
	}

	log.Printf("    running %d SET NULL queries...", len(setNullQueries))
	for _, q := range setNullQueries {
		if err := execSQL(ctx, pool, "set null orphan", q); err != nil {
			return err
		}
	}

	log.Printf("    running %d DELETE queries...", len(deleteQueries))
	for _, q := range deleteQueries {
		if err := execSQL(ctx, pool, "delete orphan", q); err != nil {
			return err
		}
	}

	return nil
}
