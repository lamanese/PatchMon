-- Default theme for new users is light instead of dark.
-- Existing users keep their stored preference (toggle in profile applies).
ALTER TABLE users ALTER COLUMN theme_preference SET DEFAULT 'light';
