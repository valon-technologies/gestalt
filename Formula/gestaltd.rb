# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.41"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.41/gestaltd-macos-arm64.tar.gz"
      sha256 "1f434621ce981f4bfb8e2ad7944149905c686e27f1a4ed299eed79f9f5bce45f"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.41/gestaltd-macos-x86_64.tar.gz"
      sha256 "f1c62a39f21f4fd79fd719269246fed57facc853edb23ce36ab8f5c6cf2cf197"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.41/gestaltd-linux-arm64.tar.gz"
      sha256 "23c36951a63b691426e6b14e0a5782db5715b27802e60ea5130cff33256d2291"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.41/gestaltd-linux-x86_64.tar.gz"
      sha256 "566775ec89bf97800021a0b051b688b9c713c4baf9cc29393baae859c446e71c"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
