# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1/gestaltd-macos-arm64.tar.gz"
      sha256 "62bfdf74e0c86f3c5273bf9f93f08ff4a16d1478b606cc84e6dd5e085bd0fdcf"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1/gestaltd-macos-x86_64.tar.gz"
      sha256 "f1c18d2173e388b4b7ec10dc7dcb2c7da9a7e6c448c392907b9175976d7be27f"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1/gestaltd-linux-arm64.tar.gz"
      sha256 "65974f64e29fe0dee1f6e4660f5b844f731cf5f3e109f03c70060ebfcbad78dd"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1/gestaltd-linux-x86_64.tar.gz"
      sha256 "679e66e28a9aaed3430938ded6f77ef64720a51186aee7a328271fe22a33db60"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
