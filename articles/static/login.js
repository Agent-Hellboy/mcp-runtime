window.handleGoogleCredential = function handleGoogleCredential(response) {
  const form = document.getElementById("google-token-form");
  const input = form?.querySelector('input[name="credential"]');
  if (!form || !input || !response?.credential) {
    return;
  }
  input.value = response.credential;
  form.submit();
};
