# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.20"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.20/gestaltd-macos-arm64.tar.gz"
      sha256 "a1543058110063675dcb2439efe254a97fbc55bc6a88ede83d1425e45a36d843"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.20/gestaltd-macos-x86_64.tar.gz"
      sha256 "ea55a585b444e244ed519f69fcf0724acdfe6e3a997459427693d0e3d7eef819"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.20/gestaltd-linux-arm64.tar.gz"
      sha256 "bcf8e4d3b59574d703b045ecf968486e630974cd4634e5c9d374f8628b77a38a"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.20/gestaltd-linux-x86_64.tar.gz"
      sha256 "8bab59923865384c2be7743ab238fa3a29c3dda9246d264bcdc0f44cb34238c0"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
