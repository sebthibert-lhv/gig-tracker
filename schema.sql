CREATE TABLE venues (
    id SERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    city TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE bands (
    id SERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE gigs (
    id SERIAL PRIMARY KEY,
    venue_id INTEGER NOT NULL REFERENCES venues(id),
    date DATE NOT NULL,
    notes TEXT,
    photo_url TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE gig_bands (
    gig_id INTEGER NOT NULL REFERENCES gigs(id) ON DELETE CASCADE,
    band_id INTEGER NOT NULL REFERENCES bands(id),
    is_headliner BOOLEAN NOT NULL DEFAULT false,
    PRIMARY KEY (gig_id, band_id)
);

CREATE UNIQUE INDEX bands_name_unique ON bands (lower(name));

CREATE UNIQUE INDEX venues_name_city_unique ON venues (lower(name), lower(coalesce(city, '')));