"""Simple Flask callback server for sip-proxy.

This returns a configurable `system_instructions` JSON used by the SIP proxy
to set the Gemini prompt for each incoming call.

Run:
  python3 -m venv .venv
  source .venv/bin/activate
  pip install flask
  python callback_server.py

POST /callback expects JSON:
  {"uri":"...","from":"...","call_id":"..."}

Response JSON must include `system_instructions` and may include `voice`.
Do NOT include a `language` field unless the model explicitly supports it.
"""

from flask import Flask, request, jsonify

app = Flask(__name__)


def select_instructions(payload: dict) -> dict:
    """Return the session config used by the proxy.

    This implementation uses a single English debt-collection prompt.
    Modify this function if you want per-caller customization.
    """

    instr = (
        "You are a professional debt-collection agent speaking on the phone. Speak naturally and briefly — act like a human agent, not a robotic script.\n"
        "When the caller responds (single sentence) do two things:\n"
        "1) Give a single, natural-sounding collection reply (no more than one sentence). If the caller shows willingness, ask politely for a repayment time. If the caller is unwilling or ambiguous, be firmer and request a clear stance.\n"
        "2) Immediately after that, output the caller's intent as one of five categories and a very short reason.\n"
        "Choose one intent category: 1=willing no time, 2=willing with time, 3=does not want to pay, 4=unclear, 5=no intention.\n"
        "Format these two lines exactly (no extra text):\n"
        "[COLLECTION] Your short reply here.\n"
        "[INTENT] <category> + brief reason\n"
        "Examples:\n"
        "[COLLECTION] Okay — when can you make the payment?\n"
        "[INTENT] 2 + promises to pay tomorrow morning\n"
        "OR\n"
        "[COLLECTION] I understand, but we need a clear answer — will you pay or not?\n"
        "[INTENT] 3 + refuses to pay currently\n"
        "Keep the live interaction under 2 minutes. After the call ends, return only these two lines again to record the final intent."
    )

    return {
        "system_instructions": instr,
        "voice": "Puck",
        # Control fields for the SIP proxy (proxy may parse these if supported)
        "end_on_intent": True,
        "intent_timeout_seconds": 30,
        "intent_end_categories": [2],
    }


@app.route("/callback", methods=["POST"])
def callback():
    try:
        payload = request.get_json(force=True)
    except Exception:
        return jsonify({"error": "invalid json"}), 400

    app.logger.info("Callback request: %s", payload)

    resp = select_instructions(payload or {})

    if "system_instructions" not in resp or not resp["system_instructions"]:
        return jsonify({"error": "missing system_instructions"}), 500

    return jsonify(resp)


@app.route("/intent", methods=["POST"])
def intent():
    try:
        data = request.get_json(force=True)
    except Exception:
        return jsonify({"error": "invalid json"}), 400

    app.logger.info("Intent notification received: %s", data)

    # Here you can implement any server-side handling: record to DB, trigger workflows, etc.

    return jsonify({"status": "ok"}), 200


@app.route("/callback/intent", methods=["POST"])
def callback_intent_alias():
    """Alias endpoint so the proxy can post to <callback_url>/intent when
    the configured callback URL includes a path (for example
    http://host:3000/callback). This simply mirrors the `/intent` handler.
    """
    try:
        data = request.get_json(force=True)
    except Exception:
        return jsonify({"error": "invalid json"}), 400

    app.logger.info("Callback/Intent notification received: %s", data)

    # Reuse same handling as /intent (keep it simple)
    return jsonify({"status": "ok"}), 200


if __name__ == "__main__":
    # Listen on all interfaces so the SIP proxy can reach it
    # Run without the reloader/debugger to avoid the double-process
    # behavior which can interfere with automated clients.
    app.run(host="0.0.0.0", port=3000, debug=False, threaded=True)
