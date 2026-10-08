-- Original order_invoice_documents rows remain immutable. Corrections and
-- confirmed refund documents append separately and retain exact PDF bytes.
CREATE TABLE v3_commerce.order_invoice_revisions (
    number text PRIMARY KEY CHECK (length(number) BETWEEN 1 AND 64),
    order_id bigint NOT NULL REFERENCES v3_commerce.order_invoice_documents(order_id),
    document_type text NOT NULL CHECK (document_type IN ('invoice','credit_note')),
    revision integer NOT NULL CHECK (revision > 0),
    related_number text NOT NULL CHECK (length(related_number) BETWEEN 1 AND 64),
    request_id text CHECK (length(request_id) BETWEEN 1 AND 128),
    reason text NOT NULL CHECK (length(btrim(reason)) BETWEEN 1 AND 300),
    buyer_name text NOT NULL CHECK (length(btrim(buyer_name)) BETWEEN 1 AND 200),
    buyer_address text NOT NULL CHECK (length(btrim(buyer_address)) BETWEEN 1 AND 600),
    buyer_country text NOT NULL DEFAULT '' CHECK (length(buyer_country)<=100),
    buyer_tax_id text NOT NULL DEFAULT '' CHECK (length(buyer_tax_id)<=64),
    seller_address text NOT NULL CHECK (length(btrim(seller_address)) BETWEEN 1 AND 600),
    amount_minor bigint NOT NULL CHECK (amount_minor>0),
    refund_total_minor bigint,
    issued_at timestamptz NOT NULL,
    snapshot jsonb NOT NULL CHECK (jsonb_typeof(snapshot)='object'),
    pdf bytea NOT NULL CHECK (octet_length(pdf)>100 AND substring(pdf FROM 1 FOR 5)=decode('255044462d','hex')),
    UNIQUE(order_id,document_type,revision),
    UNIQUE(order_id,request_id),
    UNIQUE(order_id,refund_total_minor),
    CHECK ((document_type='invoice' AND revision>=2 AND request_id IS NOT NULL AND refund_total_minor IS NULL)
        OR (document_type='credit_note' AND request_id IS NULL AND refund_total_minor IS NOT NULL AND refund_total_minor>=amount_minor))
);
