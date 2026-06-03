export interface Env {
  API_URL: string;
  WEBHOOK_SECRET: string;
  MAIL_DOMAIN?: string;
}

type EmailMessage = {
  from: string;
  to: string;
  raw: ReadableStream<Uint8Array>;
  headers: Headers;
};

const OTP_PATTERNS = [
  /\b\d{6}\b/,
  /\b\d{4,8}\b/,
  /\b[A-Z0-9]{5}\b/,
];

export default {
  async email(message: EmailMessage, env: Env): Promise<void> {
    const raw = await streamToText(message.raw);
    const subject = getHeader(message.headers, "subject") || parseHeader(raw, "subject");
    const alias = aliasFromAddress(message.to, env.MAIL_DOMAIN);
    const content = messageBody(raw).slice(0, 50_000);
    const code = extractCode(subject, content);
    const providerMessageId = getHeader(message.headers, "message-id") || parseHeader(raw, "message-id");

    await fetch(`${env.API_URL.replace(/\/+$/, "")}/mail`, {
      method: "POST",
      headers: {
        "Authorization": `Bearer ${env.WEBHOOK_SECRET}`,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        alias,
        recipient: message.to,
        sender: message.from,
        subject,
        code,
        content,
        provider_message_id: providerMessageId,
      }),
    }).then(async (response) => {
      if (!response.ok) {
        throw new Error(`API rejected mail: ${response.status} ${await response.text()}`);
      }
    });
  },
};

async function streamToText(stream: ReadableStream<Uint8Array>): Promise<string> {
  return new Response(stream).text();
}

function getHeader(headers: Headers, name: string): string {
  return headers.get(name)?.trim() || "";
}

function parseHeader(raw: string, name: string): string {
  const pattern = new RegExp(`^${escapeRegExp(name)}:\\s*(.+)$`, "im");
  return raw.match(pattern)?.[1]?.trim() || "";
}

function messageBody(raw: string): string {
  const normalized = raw.replace(/\r\n/g, "\n");
  const splitAt = normalized.indexOf("\n\n");
  return splitAt >= 0 ? normalized.slice(splitAt + 2) : normalized;
}

function aliasFromAddress(address: string, expectedDomain?: string): string {
  const email = extractEmail(address).toLowerCase();
  const [local, domain] = email.split("@");
  if (!local || !domain) {
    throw new Error(`Invalid recipient address: ${address}`);
  }
  if (expectedDomain && domain !== expectedDomain.toLowerCase()) {
    throw new Error(`Unexpected recipient domain: ${domain}`);
  }
  return local;
}

function extractEmail(value: string): string {
  const bracketed = value.match(/<([^<>@\s]+@[^<>\s]+)>/);
  if (bracketed) {
    return bracketed[1];
  }
  const bare = value.match(/[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}/i);
  if (bare) {
    return bare[0];
  }
  return value.trim();
}

function extractCode(...parts: string[]): string {
  const text = parts.filter(Boolean).join("\n");
  for (const pattern of OTP_PATTERNS) {
    const match = text.match(pattern);
    if (match) {
      return match[0];
    }
  }
  return "";
}

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}
