-- Self-service invoices are saved once per paid order, without modifying money
-- or legacy manual invoice applications. PDFs survive profile/settings changes.
CREATE TABLE v3_commerce.order_invoice_documents (
    order_id bigint PRIMARY KEY REFERENCES v3_commerce.orders(id),
    number text NOT NULL UNIQUE CHECK (length(number) BETWEEN 1 AND 64),
    buyer_name text NOT NULL CHECK (length(btrim(buyer_name)) BETWEEN 1 AND 200),
    buyer_address text NOT NULL CHECK (length(btrim(buyer_address)) BETWEEN 1 AND 600),
    seller_address text NOT NULL CHECK (length(btrim(seller_address)) BETWEEN 1 AND 600),
    issued_at timestamptz NOT NULL,
    snapshot jsonb NOT NULL CHECK (jsonb_typeof(snapshot)='object'),
    pdf bytea NOT NULL CHECK (octet_length(pdf)>100 AND substring(pdf FROM 1 FOR 5)=decode('255044462d','hex'))
);

-- Real company address supplied by the company owner. Preserve any configured
-- address on upgrade; future invoices use this setting while issued PDFs freeze it.
INSERT INTO v3_platform.settings(key,value) VALUES ('InvoiceSellerAddress',
    '"UNIT 1618A, 16/F, PIONEER CENTRE, 750 NATHAN ROAD, MONG KOK, HONG KONG"'::jsonb)
ON CONFLICT (key) DO NOTHING;
