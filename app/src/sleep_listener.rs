use crate::db::Database;
use crate::utils::get_db_path;
use chrono::Utc;
use futures_util::stream::TryStreamExt;
use zbus::{Connection, MessageStream, Result};

pub struct SleepListener;

impl SleepListener {
    pub async fn listen() -> Result<()> {
        let connection = Connection::session().await?;

        connection
            .call_method(
                Some("org.freedesktop.DBus"),
                "/org/freedesktop/DBus",
                Some("org.freedesktop.DBus.Monitoring"),
                "BecomeMonitor",
                &(&[] as &[&str], 0u32),
            )
            .await?;

        let mut stream = MessageStream::from(connection);
        while let Some(msg) = stream.try_next().await? {
            let header = msg.header();

            // Only listen to the standard freedesktop ScreenSaver (avoid duplicates)
            if header
                .member()
                .map(|m| m == "ActiveChanged")
                .unwrap_or(false)
                && header
                    .path()
                    .map(|p| p == "/org/freedesktop/ScreenSaver")
                    .unwrap_or(false)
            {
                let body = msg.body();
                if let Ok((active,)) = body.deserialize::<(bool,)>() {
                    match Database::new(&get_db_path().to_string_lossy()) {
                        Ok(db) => {
                            let now = Utc::now().timestamp();

                            if active {
                                if let Err(e) = db.finalise_active_record(now) {
                                    eprintln!("[SLEEP] Error finalising active record: {}", e);
                                }
                                if let Err(e) = db.insert_sleep_record(now) {
                                    eprintln!("[SLEEP] Error inserting sleep record: {}", e);
                                }
                            } else {
                                if let Err(e) = db.finalise_sleep_record(now) {
                                    eprintln!("[WAKE] Error finalising sleep record: {}", e);
                                }
                            }
                        }
                        Err(e) => eprintln!("[SleepListener] Failed to open database: {}", e),
                    }
                }
            }
        }
        Ok(())
    }
}
