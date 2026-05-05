# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.16"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.16/gestaltd-macos-arm64.tar.gz"
      sha256 "f420472fac6859800df434a42d5beb0bdaa6a94d61304feb50346e090991028b"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.16/gestaltd-macos-x86_64.tar.gz"
      sha256 "8b142da513a1c4002c996ab838f483e2fa89e22893a73daa9ab1a58ba5b13549"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.16/gestaltd-linux-arm64.tar.gz"
      sha256 "fabebcc5dc48ba015301dd5aa1197c09c4867ce79cd1ca3bb5cd3126d34da4de"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.16/gestaltd-linux-x86_64.tar.gz"
      sha256 "4c94bcff36d09269468042f6bd78a01845a0338539491cfbd41cb53fbf9ed6ee"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
