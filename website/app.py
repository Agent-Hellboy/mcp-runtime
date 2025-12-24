from flask import Flask, redirect, send_from_directory

app = Flask(__name__, static_folder=".", static_url_path="")


@app.route("/")
def home():
    return send_from_directory(".", "index.html")


@app.route("/docs")
def docs_redirect():
    return redirect("/docs/")


@app.route("/docs/")
def docs_index():
    return send_from_directory("docs", "index.html")


if __name__ == "__main__":
    app.run(host="0.0.0.0", port=8080)
